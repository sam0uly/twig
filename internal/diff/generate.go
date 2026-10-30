package diff

import (
	"fmt"
	"hash/maphash"
	"math"
	"strconv"
	"strings"
	"time"
)

// GenerateOptions controls how a diff is generated between two file
// contents.
type GenerateOptions struct {
	// Context is the number of unchanged lines shown around hunks.
	Context int
	// IgnoreWhitespace collapses whitespace when comparing lines; the
	// original text is still displayed.
	IgnoreWhitespace bool
}

// pageRows is the number of rows above which a diff keeps a run-length encoded
// plan and rebuilds pages on demand instead of holding every rendered line.
const pageRows = 4096

const (
	// generateBudget bounds how long one file's diff may take. Past it the
	// remaining unmatched regions are reported as wholesale replacements, which
	// is always a valid script: the field's diff tools bound their work the same
	// way (difftastic's byte and graph limits, diff-match-patch's timeout).
	generateBudget = 2 * time.Second

	// exactRows is the largest region that gets an exact Myers script. Above it
	// the region is anchored first, and only its gaps are diffed exactly.
	exactRows = 4096

	// anchorBlock is the granularity of the anchoring pre-pass, in lines. A
	// block that occurs exactly once on each side is a safe anchor, which is
	// the Bentley-McIlroy idea and what git calls patience/histogram matching.
	anchorBlock = 64

	// maxPlanLines is the largest line count a plan can address: runs store
	// their source lines in int32 fields.
	maxPlanLines = math.MaxInt32 - 1
)

// lineSeed seeds the per-process line hashing. The seed is random so a crafted
// input cannot force collisions; nothing is persisted, so it need not be stable.
var lineSeed = maphash.MakeSeed()

// Generate produces a File diff directly from two file contents, bypassing
// patch text entirely. It is the engine used by the go-git backend: line level
// differences come from anchored Myers, and word level highlights are annotated
// right after, so the whole pipeline is a single pass with no intermediate
// patch parsing. A diff with more rows than pageRows keeps a run-length encoded
// plan and rebuilds pages as they are painted.
func Generate(path string, kind ChangeKind, oldText, newText string, opts GenerateOptions) *File {
	return generate(path, kind, oldText, newText, opts, pageRows)
}

// generate is Generate with an explicit page limit. A limit of zero keeps every
// diff paged and a huge one keeps it resident, which is how the tests compare
// the two representations of the same input.
func generate(path string, kind ChangeKind, oldText, newText string, opts GenerateOptions, pageLimit int) (f *File) {
	f = &File{
		OldPath: path,
		NewPath: path,
		Kind:    kind,
	}

	// A diff engine bug must never take the whole viewer down: degrade to a
	// one-line notice instead of letting a panic escape.
	defer func() {
		if r := recover(); r != nil {
			f.Binary = false
			f.page = nil
			f.Additions, f.Deletions = 0, 0
			f.Hunks = nil
			f.Lines = []Line{{Kind: NoNewline, Text: fmt.Sprintf("diff unavailable: %v", r)}}
		}
	}()

	if isBinary(oldText) || isBinary(newText) {
		f.Binary = true
		f.Lines = []Line{{Kind: NoNewline, Text: "Binary file differs"}}
		return f
	}

	build(f, indexText(oldText), indexText(newText), opts, pageLimit)
	return f
}

// build runs the diff and lays the result out in whichever representation keeps
// memory proportional to the change rather than to the file.
func build(f *File, old, new text, opts GenerateOptions, pageLimit int) {
	// A file that only gained or only lost lines has exactly one hunk and needs
	// no matching at all: interning millions of lines to discover that is the
	// expensive half of a whole-file addition, so it is skipped outright.
	if old.n() == 0 || new.n() == 0 {
		install(f, wholeFilePlan(old, new), pageLimit)
		return
	}

	edits := lineDiff(old, new, opts.IgnoreWhitespace, time.Now().Add(generateBudget))
	windows := hunkWindows(edits, old, new, max(0, opts.Context))
	if len(windows) == 0 {
		return
	}
	install(f, buildPlan(old, new, edits, windows), pageLimit)
}

// install attaches a plan to the file in the representation it deserves.
func install(f *File, p *plan, pageLimit int) {
	if p.empty() {
		return
	}
	f.Additions, f.Deletions = p.adds, p.dels
	f.Hunks = p.hunkSkeletons()

	// A run stores its source lines in int32 fields, so a file with more lines
	// than that keeps rendered lines instead.
	plannable := p.old.n() <= maxPlanLines && p.new.n() <= maxPlanLines
	if p.rows() > pageLimit && plannable {
		f.page = p
		return
	}
	f.Lines = p.lines(0, p.rows())
	f.attachHunks()
}

// wholeFilePlan is the plan of a file that exists on only one side: one hunk,
// one run. It is O(1) work no matter how many lines the file has.
func wholeFilePlan(old, new text) *plan {
	p := &plan{old: old, new: new, headers: make([]string, 0, 1), hunks: make([]int32, 0, 1)}
	switch {
	case old.n() == 0 && new.n() == 0:
		return p
	case old.n() == 0:
		p.ops = []op{{kind: opHeader, oldStart: 0}, {kind: opInsert, newLen: int32(new.n())}}
		p.headers = append(p.headers, hunkHeader(0, 0, 1, new.n()))
		p.adds = new.n()
	default:
		p.ops = []op{{kind: opHeader, oldStart: 0}, {kind: opDelete, oldLen: int32(old.n())}}
		p.headers = append(p.headers, hunkHeader(1, old.n(), 0, 0))
		p.dels = old.n()
	}
	p.hunks = append(p.hunks, 0)
	p.buildPrefix()
	return p
}

// isBinary reports whether content looks binary (NUL byte in the first 8KB).
func isBinary(s string) bool {
	n := min(len(s), 8192)
	return strings.IndexByte(s[:n], 0) >= 0
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

type editKind uint8

const (
	editEqual editKind = iota
	editInsert
	editDelete
)

// edit is one step of the line-level difference: a kind plus the two source
// line cursors, which stay 32 bit so a multi-million line script stays small.
type edit struct {
	kind   editKind
	oldIdx int32 // index into the old text for equal/delete
	newIdx int32 // index into the new text for equal/insert
}

// lineDiff computes the edit script between two indexed texts. It trims the
// common ends, then diffs the middle by anchoring: identical blocks of lines are
// matched first (Bentley-McIlroy; the idea git credits as patience/histogram),
// and only the unmatched gaps get an exact Myers script. Work and memory are
// therefore proportional to the change, not to the file, and a region with no
// anchors is split until it is small enough to diff exactly.
// diffState carries everything one file's diff needs across its regions: the two
// sources, a line hash per line (computed once, so a recursive split never
// rehashes the file), and the work budget.
type diffState struct {
	old, new text
	oldH     []uint64 // nil when the file is small enough to hash inline
	newH     []uint64
	ignoreWS bool
	deadline time.Time
	out      []edit
}

// lineDiff computes the edit script between two indexed texts. It trims the
// common ends, then diffs the middle by anchoring: identical blocks of lines are
// matched first (Bentley-McIlroy; the idea git credits as patience/histogram),
// and only the unmatched gaps get an exact Myers script. Work and memory are
// therefore proportional to the change, not to the file, and a region with no
// anchors is split until it is small enough to diff exactly.
func lineDiff(old, new text, ignoreWS bool, deadline time.Time) []edit {
	p := 0
	for p < old.n() && p < new.n() && sameLine(old.line(p), new.line(p), ignoreWS) {
		p++
	}
	sfx := 0
	for sfx < old.n()-p && sfx < new.n()-p &&
		sameLine(old.line(old.n()-1-sfx), new.line(new.n()-1-sfx), ignoreWS) {
		sfx++
	}

	st := &diffState{old: old, new: new, ignoreWS: ignoreWS, deadline: deadline}
	st.out = make([]edit, 0, p+sfx+16)
	for i := range p {
		st.out = append(st.out, edit{kind: editEqual, oldIdx: int32(i), newIdx: int32(i)})
	}

	// Hash the middle once. A split recurses over the same lines many times, and
	// hashing them each time is what dominates a large rewrite.
	ahi, bhi := old.n()-sfx, new.n()-sfx
	if ahi-p > exactRows || bhi-p > exactRows {
		st.oldH = hashLines(old, p, ahi, ignoreWS)
		st.newH = hashLines(new, p, bhi, ignoreWS)
	}
	st.gap(p, ahi, p, bhi)

	for i := range sfx {
		st.out = append(st.out, edit{
			kind:   editEqual,
			oldIdx: int32(old.n() - sfx + i),
			newIdx: int32(new.n() - sfx + i),
		})
	}
	return st.out
}

// hashLines hashes lines [from, to) of one side into a slice indexed by line.
// Entries outside the range stay zero and are never read.
func hashLines(t text, from, to int, ignoreWS bool) []uint64 {
	h := make([]uint64, t.n())
	for i := from; i < to; i++ {
		line := t.line(i)
		if ignoreWS {
			line = normalizeWS(line)
		}
		h[i] = maphash.String(lineSeed, line)
	}
	return h
}

// hashOld is the hash of old line i.
func (st *diffState) hashOld(i int) uint64 {
	if st.oldH != nil {
		return st.oldH[i]
	}
	return st.hashLine(st.old.line(i))
}

// hashNew is the hash of new line i.
func (st *diffState) hashNew(i int) uint64 {
	if st.newH != nil {
		return st.newH[i]
	}
	return st.hashLine(st.new.line(i))
}

// hashLine hashes one line, normalising it when whitespace is ignored.
func (st *diffState) hashLine(s string) uint64 {
	if st.ignoreWS {
		s = normalizeWS(s)
	}
	return maphash.String(lineSeed, s)
}

// expired reports whether the work budget is spent.
func (st *diffState) expired() bool { return !time.Now().Before(st.deadline) }

// gap appends the script for old[alo:ahi] against new[blo:bhi].
func (st *diffState) gap(alo, ahi, blo, bhi int) {
	old, new := st.old, st.new
	for alo < ahi && blo < bhi && sameLine(old.line(alo), new.line(blo), st.ignoreWS) {
		st.out = append(st.out, edit{kind: editEqual, oldIdx: int32(alo), newIdx: int32(blo)})
		alo++
		blo++
	}
	suffix := 0
	for alo < ahi && blo < bhi && sameLine(old.line(ahi-1), new.line(bhi-1), st.ignoreWS) {
		ahi--
		bhi--
		suffix++
	}

	switch {
	case alo == ahi:
		for ; blo < bhi; blo++ {
			st.out = append(st.out, edit{kind: editInsert, oldIdx: int32(alo), newIdx: int32(blo)})
		}
	case blo == bhi:
		for ; alo < ahi; alo++ {
			st.out = append(st.out, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
		}
	case ahi-alo <= exactRows && bhi-blo <= exactRows:
		// The budget is checked here as well as in the anchoring branch: a file
		// can be split into thousands of small regions, and each one would
		// otherwise still be worth an exact script.
		if st.expired() {
			st.coarse(alo, ahi, blo, bhi)
			break
		}
		st.exact(alo, ahi, blo, bhi)
	default:
		if st.expired() {
			st.coarse(alo, ahi, blo, bhi)
			break
		}
		pairs := st.anchors(alo, ahi, blo, bhi)
		if len(pairs) == 0 {
			// Nothing to anchor on: halve the region so every step shrinks it,
			// and stop at exactRows where Myers takes over.
			am := alo + (ahi-alo)/2
			bm := blo + (bhi-blo)/2
			st.gap(alo, am, blo, bm)
			st.gap(am, ahi, bm, bhi)
			break
		}
		pa, pb := alo, blo
		for _, pair := range pairs {
			st.gap(pa, pair[0], pb, pair[1])
			for i := range anchorBlock {
				st.out = append(st.out, edit{
					kind:   editEqual,
					oldIdx: int32(pair[0] + i),
					newIdx: int32(pair[1] + i),
				})
			}
			pa, pb = pair[0]+anchorBlock, pair[1]+anchorBlock
		}
		st.gap(pa, ahi, pb, bhi)
	}

	for i := range suffix {
		st.out = append(st.out, edit{kind: editEqual, oldIdx: int32(ahi + i), newIdx: int32(bhi + i)})
	}
}

// coarse reports a region as a wholesale replacement, which is always a valid
// script and needs no matching at all.
func (st *diffState) coarse(alo, ahi, blo, bhi int) {
	for ; alo < ahi; alo++ {
		st.out = append(st.out, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
	}
	for ; blo < bhi; blo++ {
		st.out = append(st.out, edit{kind: editInsert, oldIdx: int32(ahi), newIdx: int32(blo)})
	}
}

// exact diffs one region exactly: intern both sides into dense ids, run the
// bounded Myers match, and let it fall back to anchoring an intractable region
// on unique lines.
func (st *diffState) exact(alo, ahi, blo, bhi int) {
	a, b := st.ids(alo, ahi, blo, bhi)
	for _, e := range diffIDs(a, b) {
		e.oldIdx += int32(alo)
		e.newIdx += int32(blo)
		st.out = append(st.out, e)
	}
}

// ids interns the lines of two ranges into dense ids keyed by their hash and
// verified against the line it came from, so a collision moves to the next probe
// slot instead of quietly equating two different lines. Unlike a string-keyed
// map the keys are eight bytes and no source line is retained.
func (st *diffState) ids(alo, ahi, blo, bhi int) ([]int32, []int32) {
	byKey := make(map[uint64]int32, (ahi-alo)+(bhi-blo))
	texts := make([]string, 0, (ahi-alo)+(bhi-blo))

	get := func(h uint64, s string) int32 {
		for probe := 0; ; probe++ {
			id, ok := byKey[lineKey(h, probe)]
			if !ok {
				id = int32(len(texts))
				byKey[lineKey(h, probe)] = id
				texts = append(texts, s)
				return id
			}
			if texts[id] == s {
				return id
			}
		}
	}

	norm := func(s string) string {
		if st.ignoreWS {
			return normalizeWS(s)
		}
		return s
	}

	x := make([]int32, ahi-alo)
	for i := range x {
		line := alo + i
		x[i] = get(st.hashOld(line), norm(st.old.line(line)))
	}
	y := make([]int32, bhi-blo)
	for i := range y {
		line := blo + i
		y[i] = get(st.hashNew(line), norm(st.new.line(line)))
	}
	return x, y
}

// lineKey is the map key for the probe-th candidate of a line hash.
func lineKey(h uint64, probe int) uint64 {
	if probe == 0 {
		return h
	}
	return h + uint64(probe)*0x9E3779B97F4A7C15
}

// blockStat is what the anchoring pass counts per block hash.
type blockStat struct {
	oldCount, newCount uint32
	newFirst           int32
}

// anchors returns ordered pairs of identical, non-overlapping anchorBlock line
// blocks, as start offsets in the old and new texts. A block that occurs exactly
// once on each side is a safe anchor; its contents are verified so a hash
// collision can never claim two different blocks are equal.
func (st *diffState) anchors(alo, ahi, blo, bhi int) [][2]int {
	if ahi-alo < 2*anchorBlock || bhi-blo < 2*anchorBlock {
		return nil
	}

	oldStarts := blockStarts(alo, ahi)
	newStarts := blockStarts(blo, bhi)
	stats := make(map[uint64]blockStat, len(oldStarts)+len(newStarts))

	for _, start := range oldStarts {
		h := st.hashBlock(st.hashOld, start)
		s := stats[h]
		s.oldCount++
		stats[h] = s
	}
	for i, start := range newStarts {
		h := st.hashBlock(st.hashNew, start)
		s := stats[h]
		s.newCount++
		if s.newCount == 1 {
			s.newFirst = int32(i)
		}
		stats[h] = s
	}

	cand := make([][2]int, 0, len(oldStarts))
	for _, start := range oldStarts {
		s, ok := stats[st.hashBlock(st.hashOld, start)]
		if !ok || s.oldCount != 1 || s.newCount != 1 {
			continue
		}
		other := newStarts[s.newFirst]
		if !sameBlock(st.old, start, st.new, other, st.ignoreWS) {
			continue
		}
		cand = append(cand, [2]int{start, other})
	}
	if len(cand) < 1 {
		return nil
	}
	return longestIncreasing(cand)
}

// blockStarts lists the first line of every whole anchorBlock in a range.
func blockStarts(from, to int) []int {
	n := (to - from) / anchorBlock
	out := make([]int, n)
	for i := range out {
		out[i] = from + i*anchorBlock
	}
	return out
}

// hashBlock is the hash of anchorBlock lines starting at start, combined from
// the per-line hashes.
func (st *diffState) hashBlock(hash func(int) uint64, start int) uint64 {
	h := uint64(0)
	for i := range anchorBlock {
		h = h*0x100000001B3 + hash(start+i)
	}
	return h
}

// sameBlock compares two blocks line by line, which is what makes an anchor
// trustworthy rather than merely likely.
func sameBlock(old text, oldStart int, new text, newStart int, ignoreWS bool) bool {
	for i := range anchorBlock {
		if !sameLine(old.line(oldStart+i), new.line(newStart+i), ignoreWS) {
			return false
		}
	}
	return true
}

// coarseThreshold is the size below which a region that cannot be matched is
// reported as a wholesale replacement instead of being split again.
const coarseThreshold = 64

// diffIDs returns the minimal-ish edit script between two interned sequences.
// It trims the common prefix and suffix, runs Myers on what is left, and, when
// the edit distance is too large to trace, splits the region on unique lines
// (patience-style anchors) and recurses into the gaps. The result is always a
// valid script; it stays minimal for every region Myers can reach.
func diffIDs(a, b []int32) []edit {
	var es []edit
	diffRegion(a, b, 0, len(a), 0, len(b), &es)
	return es
}

func diffRegion(a, b []int32, alo, ahi, blo, bhi int, out *[]edit) {
	for alo < ahi && blo < bhi && a[alo] == b[blo] {
		*out = append(*out, edit{kind: editEqual, oldIdx: int32(alo), newIdx: int32(blo)})
		alo++
		blo++
	}
	suffix := 0
	for alo < ahi && blo < bhi && a[ahi-1] == b[bhi-1] {
		ahi--
		bhi--
		suffix++
	}

	switch {
	case alo == ahi || blo == bhi:
		for ; alo < ahi; alo++ {
			*out = append(*out, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
		}
		for ; blo < bhi; blo++ {
			*out = append(*out, edit{kind: editInsert, oldIdx: int32(alo), newIdx: int32(blo)})
		}
	default:
		if es, ok := myersEdits(a[alo:ahi], b[blo:bhi], myersCap(ahi-alo, bhi-blo)); ok {
			for _, e := range es {
				e.oldIdx += int32(alo)
				e.newIdx += int32(blo)
				*out = append(*out, e)
			}
			break
		}
		diffAnchored(a, b, alo, ahi, blo, bhi, out)
	}

	for i := range suffix {
		*out = append(*out, edit{kind: editEqual, oldIdx: int32(ahi + i), newIdx: int32(bhi + i)})
	}
}

// diffAnchored splits an oversized region on lines that occur exactly once on
// each side, then diffs the gaps. Without anchors it halves the region, so every
// recursion step strictly shrinks the problem.
func diffAnchored(a, b []int32, alo, ahi, blo, bhi int, out *[]edit) {
	pairs := anchorPairs(a, alo, ahi, b, blo, bhi)
	if len(pairs) == 0 {
		if ahi-alo+bhi-blo <= coarseThreshold {
			for ; alo < ahi; alo++ {
				*out = append(*out, edit{kind: editDelete, oldIdx: int32(alo), newIdx: int32(blo)})
			}
			for ; blo < bhi; blo++ {
				*out = append(*out, edit{kind: editInsert, oldIdx: int32(ahi), newIdx: int32(blo)})
			}
			return
		}
		am := alo + (ahi-alo)/2
		bm := blo + (bhi-blo)/2
		diffRegion(a, b, alo, am, blo, bm, out)
		diffRegion(a, b, am, ahi, bm, bhi, out)
		return
	}

	pa, pb := alo, blo
	for _, p := range pairs {
		diffRegion(a, b, pa, p[0], pb, p[1], out)
		*out = append(*out, edit{kind: editEqual, oldIdx: int32(p[0]), newIdx: int32(p[1])})
		pa, pb = p[0]+1, p[1]+1
	}
	diffRegion(a, b, pa, ahi, pb, bhi, out)
}

// anchorPairs returns the longest order-preserving list of lines that appear
// exactly once in both ranges.
func anchorPairs(a []int32, alo, ahi int, b []int32, blo, bhi int) [][2]int {
	countA := make(map[int32]int, ahi-alo)
	for i := alo; i < ahi; i++ {
		countA[a[i]]++
	}
	countB := make(map[int32]int, bhi-blo)
	firstB := make(map[int32]int, bhi-blo)
	for j := blo; j < bhi; j++ {
		id := b[j]
		countB[id]++
		if countB[id] == 1 {
			firstB[id] = j
		}
	}

	var cand [][2]int
	for i := alo; i < ahi; i++ {
		id := a[i]
		if countA[id] != 1 || countB[id] != 1 {
			continue
		}
		cand = append(cand, [2]int{i, firstB[id]})
	}
	if len(cand) < 2 {
		return cand
	}
	return longestIncreasing(cand)
}

// longestIncreasing keeps the longest subsequence of pairs whose second
// coordinate also increases, which is the patience-diff LIS step.
func longestIncreasing(pairs [][2]int) [][2]int {
	tails := make([]int, 0, len(pairs)) // positions in pairs
	prev := make([]int, len(pairs))
	for i := range prev {
		prev[i] = -1
	}

	for i, p := range pairs {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if pairs[tails[mid]][1] < p[1] {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			prev[i] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}

	out := make([][2]int, len(tails))
	for i, k := len(tails)-1, tails[len(tails)-1]; i >= 0; i, k = i-1, prev[k] {
		out[i] = pairs[k]
	}
	return out
}

// editValid reports whether both sides of an edit are in range.
func editValid(e edit, old, new text) bool {
	switch e.kind {
	case editEqual:
		return e.oldIdx >= 0 && int(e.oldIdx) < old.n() && e.newIdx >= 0 && int(e.newIdx) < new.n()
	case editDelete:
		return e.oldIdx >= 0 && int(e.oldIdx) < old.n()
	default:
		return e.newIdx >= 0 && int(e.newIdx) < new.n()
	}
}

// hunkWindow is a run of aligned rows that one hunk covers: its header row
// plus the body, in aligned-stream coordinates.
type hunkWindow struct{ start, end int }

// hunkWindows locates the hunks in the aligned stream, exactly like git's
// unified format with context lines, then slides each one to a nicer boundary.
// It is the first of the two passes: nothing is emitted until the layout is
// known, so rows can go straight into their final place.
func hunkWindows(edits []edit, old, new text, context int) []hunkWindow {
	valid := validEdits(edits, old, new)
	if len(valid) == 0 {
		return nil
	}
	windows := naiveWindows(valid, context)
	slideWindows(windows, valid, old, new, context)
	return windows
}

// naiveWindows groups the changes into hunks with the configured context, the
// way git's unified format does before any boundary tuning.
func naiveWindows(valid []edit, context int) []hunkWindow {
	var windows []hunkWindow
	for i, e := range valid {
		if e.kind == editEqual {
			continue
		}
		start := max(0, i-context)
		end := min(len(valid), i+context+1)
		if n := len(windows); n > 0 && start <= windows[n-1].end {
			windows[n-1].end = end
			continue
		}
		windows = append(windows, hunkWindow{start: start, end: end})
	}
	return windows
}

// validEdits keeps the edits that address real lines: that aligned row stream is
// what every window index refers to.
func validEdits(edits []edit, old, new text) []edit {
	out := make([]edit, 0, len(edits))
	for _, e := range edits {
		if editValid(e, old, new) {
			out = append(out, e)
		}
	}
	return out
}

// slideWindows shifts each hunk within the context it can spare, so its
// boundaries land where a reader would put them: right after a blank line or at
// a dedent, rather than in the middle of a block. This is git's indent
// heuristic applied to the context window; a hunk only ever travels over
// context rows, so the change rows inside it never move. The travel is capped at
// half the context, so a hunk never trades away all of its leading context.
func slideWindows(windows []hunkWindow, valid []edit, old, new text, context int) {
	limit := max(1, (context+1)/2)
	for i := range windows {
		w := &windows[i]
		prevEnd := -1
		if i > 0 {
			prevEnd = windows[i-1].end
		}
		nextStart := len(valid)
		if i+1 < len(windows) {
			nextStart = windows[i+1].start
		}

		maxUp := 0
		for maxUp < limit && w.start-maxUp-1 > prevEnd &&
			valid[w.start-maxUp-1].kind == editEqual &&
			valid[w.end-maxUp-1].kind == editEqual {
			maxUp++
		}
		maxDown := 0
		for maxDown < limit && w.end+maxDown < nextStart &&
			valid[w.end+maxDown].kind == editEqual &&
			valid[w.start+maxDown].kind == editEqual {
			maxDown++
		}

		best, bestScore := 0, slideScore(0, *w, valid, old, new)
		for step := 1; step <= max(maxUp, maxDown); step++ {
			for _, k := range [2]int{-step, step} {
				if k < -maxUp || k > maxDown {
					continue
				}
				if s := slideScore(k, *w, valid, old, new); s > bestScore {
					best, bestScore = k, s
				}
			}
		}
		w.start += best
		w.end += best
	}
}

// slideScore rates a window shifted by k rows.
func slideScore(k int, w hunkWindow, valid []edit, old, new text) int {
	start, end := w.start+k, w.end+k
	score := 0
	if start > 0 {
		score += boundaryScore(rowLine(valid, start-1, old, new), rowLine(valid, start, old, new))
	}
	if end < len(valid) {
		score += boundaryScore(rowLine(valid, end-1, old, new), rowLine(valid, end, old, new))
	}
	return score
}

// boundaryScore rates how natural the cut between two lines looks. A blank line
// is the best boundary, a dedent the next best, and an unchanged indentation is
// still better than nothing.
func boundaryScore(prev, next string) int {
	switch {
	case strings.TrimSpace(next) == "":
		return 3
	case indentOf(next) < indentOf(prev):
		return 2
	case indentOf(next) == indentOf(prev):
		return 1
	default:
		return 0
	}
}

// indentOf is the display width of a line's leading whitespace, with a tab
// counted as four columns.
func indentOf(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// rowLine is the source line an aligned row shows, without its diff sign.
func rowLine(valid []edit, row int, old, new text) string {
	e := valid[row]
	if e.kind == editInsert {
		return new.line(int(e.newIdx))
	}
	return old.line(int(e.oldIdx))
}

// buildPlan turns the edit script into a run-length encoded plan, one hunk at a
// time. Consecutive edits of the same kind become one run, and a deletion run
// followed by an addition run becomes one replacement run so word highlighting
// can pair them.
func buildPlan(old, new text, edits []edit, windows []hunkWindow) *plan {
	p := &plan{
		old:     old,
		new:     new,
		ops:     make([]op, 0, 2*len(windows)),
		headers: make([]string, 0, len(windows)),
		hunks:   make([]int32, 0, len(windows)),
	}
	runs := make([]op, 0, 8)

	ei, aligned := 0, 0
	for _, w := range windows {
		for aligned < w.start && ei < len(edits) {
			if editValid(edits[ei], old, new) {
				aligned++
			}
			ei++
		}

		header := len(p.ops)
		p.ops = append(p.ops, op{kind: opHeader})
		p.hunks = append(p.hunks, int32(header))

		var oldStart, newStart, oldCount, newCount int
		runs = runs[:0]
		for aligned < w.end && ei < len(edits) {
			e := edits[ei]
			ei++
			if !editValid(e, old, new) {
				continue
			}
			aligned++

			switch e.kind {
			case editEqual:
				if oldStart == 0 {
					oldStart = int(e.oldIdx) + 1
				}
				oldCount++
				if newStart == 0 {
					newStart = int(e.newIdx) + 1
				}
				newCount++
				if n := len(runs); n > 0 && runs[n-1].kind == opEqual {
					runs[n-1].oldLen++
					runs[n-1].newLen++
				} else {
					runs = append(runs, op{kind: opEqual, oldStart: e.oldIdx, newStart: e.newIdx, oldLen: 1, newLen: 1})
				}

			case editDelete:
				if oldStart == 0 {
					oldStart = int(e.oldIdx) + 1
				}
				oldCount++
				if n := len(runs); n > 0 && runs[n-1].kind == opDelete {
					runs[n-1].oldLen++
				} else {
					runs = append(runs, op{kind: opDelete, oldStart: e.oldIdx, newStart: e.newIdx, oldLen: 1})
				}

			default:
				if newStart == 0 {
					newStart = int(e.newIdx) + 1
				}
				newCount++
				if n := len(runs); n > 0 && runs[n-1].kind == opInsert {
					runs[n-1].newLen++
				} else {
					runs = append(runs, op{kind: opInsert, oldStart: e.oldIdx, newStart: e.newIdx, newLen: 1})
				}
			}
		}

		p.headers = append(p.headers, hunkHeader(oldStart, oldCount, newStart, newCount))
		p.ops[header] = op{kind: opHeader, oldStart: int32(len(p.headers) - 1)}
		p.ops = append(p.ops, mergeReplacements(runs)...)
	}

	p.buildPrefix()
	for _, o := range p.ops {
		switch o.kind {
		case opDelete:
			p.dels += int(o.oldLen)
		case opInsert:
			p.adds += int(o.newLen)
		case opReplace:
			p.dels += int(o.oldLen)
			p.adds += int(o.newLen)
		}
	}
	return p
}

// mergeReplacements folds a deletion run and the addition run that follows it
// into one replacement run: split mode pairs the two sides, and the word diff
// needs both halves in one unit of rows.
func mergeReplacements(runs []op) []op {
	out := runs[:0]
	for i := 0; i < len(runs); i++ {
		if runs[i].kind == opDelete && i+1 < len(runs) && runs[i+1].kind == opInsert {
			out = append(out, op{
				kind:     opReplace,
				oldStart: runs[i].oldStart,
				oldLen:   runs[i].oldLen,
				newStart: runs[i+1].newStart,
				newLen:   runs[i+1].newLen,
			})
			i++
			continue
		}
		out = append(out, runs[i])
	}
	return out
}

// hunkSkeletons is the hunk list of a paged diff: positions and headers only,
// the bodies are rebuilt when they are painted.
func (p *plan) hunkSkeletons() []Hunk {
	hunks := make([]Hunk, len(p.hunks))
	for i := range hunks {
		hunks[i] = Hunk{Header: p.headers[p.headerIndex(i)], LineIndex: p.hunkLineIndex(i)}
	}
	return hunks
}

// headerIndex is the index in plan.headers of hunk i's header text.
func (p *plan) headerIndex(i int) int32 {
	return p.ops[p.hunks[i]].oldStart
}

// attachHunks fills in the body slices of a file whose lines are resident.
func (f *File) attachHunks() {
	for i := range f.Hunks {
		end := len(f.Lines)
		if i+1 < len(f.Hunks) {
			end = f.Hunks[i+1].LineIndex
		}
		f.Hunks[i].Lines = f.Lines[min(f.Hunks[i].LineIndex+1, end):end]
	}
}

func hunkHeader(oldStart, oldCount, nStart, newCount int) string {
	// Starts are 1-based already; a zero-count side reports 0, like git.
	oldStart, newStart := oldStart, nStart
	if oldCount == 0 {
		oldStart = 0
	}
	if newCount == 0 {
		newStart = 0
	}
	o := strconv.Itoa(oldStart)
	if oldCount != 1 {
		o += "," + strconv.Itoa(oldCount)
	}
	n := strconv.Itoa(newStart)
	if newCount != 1 {
		n += "," + strconv.Itoa(newCount)
	}
	return fmt.Sprintf("@@ -%s +%s @@", o, n)
}
