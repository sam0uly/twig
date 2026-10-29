package diff

import (
	"sort"
	"strings"
)

// A diff plan is the rendered row stream, run-length encoded. Each entry covers
// a whole run of rows: one hunk header, a run of context, a run of deletions or
// a run of additions. The field's viewers store a row per rendered line; a plan
// stores a run per run, so a file that was added whole costs one entry no
// matter how many lines it has, and a normal code diff costs three or four
// entries per hunk. A per-entry prefix index gives O(log runs) lookup from a
// row to the run that renders it.
//
// Rendering a row never needs a stored counter: the run carries the source
// line it starts at, so row j of an equal run is old.line(oldStart+j) and the
// line numbers fall out of the same arithmetic.
//
// The representation follows the range-encoded edit scripts used by git's xdiff
// and by similar/diff-match-patch, which encode (Equal|Delete|Insert, ranges)
// instead of an item per element.

// opKind is the kind of one run of rendered rows.
type opKind uint8

const (
	// opEqual is a run of context rows: both sides advance together.
	opEqual opKind = iota
	// opDelete is a run of deletion rows: only the old side advances.
	opDelete
	// opInsert is a run of addition rows: only the new side advances.
	opInsert
	// opReplace is a rewritten block: its deletion rows then its addition rows.
	// Keeping both halves in one run is what lets split mode pair them and the
	// word diff compare them.
	opReplace
	// opHeader is a single row holding a hunk header.
	opHeader
)

// op is one run of rendered rows. Indices are 0-based source line numbers; a
// header stores the index of its text in plan.headers instead.
type op struct {
	kind             opKind
	oldStart, oldLen int32
	newStart, newLen int32
}

// rows is how many rendered rows the run covers.
func (o op) rows() int {
	switch o.kind {
	case opHeader:
		return 1
	case opDelete:
		return int(o.oldLen)
	case opInsert:
		return int(o.newLen)
	case opReplace:
		return int(o.oldLen) + int(o.newLen)
	default:
		return int(o.oldLen)
	}
}

// plan rebuilds the rendered rows of a diff from its two sources.
type plan struct {
	old, new text
	ops      []op
	prefix   []int32 // prefix[i] is the first row of ops[i]; one longer than ops
	headers  []string
	hunks    []int32 // entry index of each hunk header, in order
	adds     int
	dels     int
}

// planBytes is the bookkeeping one run keeps: its ranges plus its share of the
// prefix index.
const planBytes = 20

// bytes is the memory the plan keeps resident. Runs are proportional to the
// change, not to the file, so a three million line addition is a handful of
// entries on top of its sources.
func (p *plan) bytes() int {
	n := len(p.ops)*planBytes + len(p.prefix)*4 + len(p.hunks)*4 + p.old.bytes() + p.new.bytes()
	for _, h := range p.headers {
		n += len(h)
	}
	return n
}

// rows is the number of rendered rows.
func (p *plan) rows() int {
	if len(p.prefix) == 0 {
		return 0
	}
	return int(p.prefix[len(p.prefix)-1])
}

// entry returns the index of the run containing row.
func (p *plan) entry(row int) int {
	i := sort.Search(len(p.ops), func(i int) bool { return int(p.prefix[i+1]) > row })
	if i >= len(p.ops) {
		return len(p.ops) - 1
	}
	return i
}

// lines rebuilds rows [from, to) as rendered lines. A range that cuts a change
// run in half leaves that edge without word highlights, because the run's
// partner may sit outside the page; the cut points are the caller's block
// boundaries, so a row always looks the same whichever page it lands on.
func (p *plan) lines(from, to int) []Line {
	from, to = max(0, from), min(to, p.rows())
	if from >= to {
		return nil
	}

	out := make([]Line, 0, to-from)
	for i := from; i < to; {
		ei := p.entry(i)
		o := p.ops[ei]
		start := int(p.prefix[ei])
		off, left := i-start, to-i

		switch o.kind {
		case opHeader:
			out = append(out, Line{Kind: HunkHeader, Text: p.headers[o.oldStart]})
			i++

		case opEqual:
			n := min(int(o.oldLen)-off, left)
			for j := range n {
				oldLine := int(o.oldStart) + off + j
				out = append(out, Line{
					Kind: Context,
					Text: " " + p.old.line(oldLine),
					Old:  oldLine + 1,
					New:  int(o.newStart) + off + j + 1,
				})
			}
			i += n

		case opDelete:
			n := min(int(o.oldLen)-off, left)
			for j := range n {
				oldLine := int(o.oldStart) + off + j
				out = append(out, Line{
					Kind: Deletion,
					Text: "-" + p.old.line(oldLine),
					Old:  oldLine + 1,
				})
			}
			i += n

		case opInsert:
			n := min(int(o.newLen)-off, left)
			for j := range n {
				newLine := int(o.newStart) + off + j
				out = append(out, Line{
					Kind: Addition,
					Text: "+" + p.new.line(newLine),
					New:  newLine + 1,
				})
			}
			i += n

		case opReplace:
			// The deletions of a rewritten block, then its additions, in one
			// unit so the word diff sees both sides at once.
			body := min(left, o.rows()-off)
			delOff, addOff := off, 0
			if off > int(o.oldLen) {
				delOff, addOff = int(o.oldLen), off-int(o.oldLen)
			}
			nd := min(int(o.oldLen)-delOff, body)
			for j := range nd {
				oldLine := int(o.oldStart) + delOff + j
				out = append(out, Line{Kind: Deletion, Text: "-" + p.old.line(oldLine), Old: oldLine + 1})
			}
			for j := range body - nd {
				newLine := int(o.newStart) + addOff + j
				out = append(out, Line{Kind: Addition, Text: "+" + p.new.line(newLine), New: newLine + 1})
			}
			i += body
		}
	}

	// Word highlighting is decided per hunk, not per run: an edit script can
	// interleave deletions and additions, and the hunk is what the reader sees.
	annotateLines(out,
		from > 0 && p.changeAt(from-1),
		to < p.rows() && p.changeAt(to))
	return out
}

// changeAt reports whether row i belongs to a change run.
func (p *plan) changeAt(i int) bool {
	switch p.kindAt(i) {
	case Addition, Deletion:
		return true
	default:
		return false
	}
}

// kindAt reports the kind of row i, without building it.
func (p *plan) kindAt(i int) LineKind {
	if i < 0 || i >= p.rows() {
		return Context
	}
	ei := p.entry(i)
	o := p.ops[ei]
	switch o.kind {
	case opHeader:
		return HunkHeader
	case opDelete:
		return Deletion
	case opInsert:
		return Addition
	case opReplace:
		if i-int(p.prefix[ei]) < int(o.oldLen) {
			return Deletion
		}
		return Addition
	default:
		return Context
	}
}

// textAt is the rendered text of row i, without building the line around it.
func (p *plan) textAt(i int) string {
	if i < 0 || i >= p.rows() {
		return ""
	}
	ei := p.entry(i)
	o := p.ops[ei]
	off := i - int(p.prefix[ei])

	switch o.kind {
	case opHeader:
		return p.headers[o.oldStart]
	case opDelete:
		return "-" + p.old.line(int(o.oldStart)+off)
	case opInsert:
		return "+" + p.new.line(int(o.newStart)+off)
	case opReplace:
		if off < int(o.oldLen) {
			return "-" + p.old.line(int(o.oldStart)+off)
		}
		return "+" + p.new.line(int(o.newStart)+off-int(o.oldLen))
	default:
		return " " + p.old.line(int(o.oldStart)+off)
	}
}

// scan walks rows [from, to) reporting each row's kind and rendered width
// without building it, which is how a document lays millions of rows out
// without allocating.
func (p *plan) scan(from, to int, fn func(kind LineKind, width int)) {
	from, to = max(0, from), min(to, p.rows())
	for i := from; i < to; {
		ei := p.entry(i)
		o := p.ops[ei]
		start := int(p.prefix[ei])
		off, left := i-start, to-i

		switch o.kind {
		case opHeader:
			fn(HunkHeader, len(p.headers[o.oldStart]))
			i++

		case opEqual:
			n := min(int(o.oldLen)-off, left)
			for j := range n {
				fn(Context, 1+p.old.lineLen(int(o.oldStart)+off+j))
			}
			i += n

		case opDelete:
			n := min(int(o.oldLen)-off, left)
			for j := range n {
				fn(Deletion, 1+p.old.lineLen(int(o.oldStart)+off+j))
			}
			i += n

		case opInsert:
			n := min(int(o.newLen)-off, left)
			for j := range n {
				fn(Addition, 1+p.new.lineLen(int(o.newStart)+off+j))
			}
			i += n

		case opReplace:
			body := min(left, o.rows()-off)
			delOff, addOff := off, 0
			if off > int(o.oldLen) {
				delOff, addOff = int(o.oldLen), off-int(o.oldLen)
			}
			nd := min(int(o.oldLen)-delOff, body)
			for j := range nd {
				fn(Deletion, 1+p.old.lineLen(int(o.oldStart)+delOff+j))
			}
			for j := range body - nd {
				fn(Addition, 1+p.new.lineLen(int(o.newStart)+addOff+j))
			}
			i += body
		}
	}
}

// hunkEntry returns the entry index of hunk i's header.
func (p *plan) hunkEntry(i int) (int, bool) {
	if i < 0 || i >= len(p.hunks) {
		return 0, false
	}
	return int(p.hunks[i]), true
}

// hunkLineIndex is the row of hunk i's header.
func (p *plan) hunkLineIndex(i int) int {
	ei, ok := p.hunkEntry(i)
	if !ok {
		return 0
	}
	return int(p.prefix[ei])
}

// hunkRows is how many rows hunk i's body covers.
func (p *plan) hunkRows(i int) int {
	ei, ok := p.hunkEntry(i)
	if !ok {
		return 0
	}
	end := len(p.ops)
	if i+1 < len(p.hunks) {
		end = int(p.hunks[i+1])
	}
	return int(p.prefix[end]) - int(p.prefix[ei]) - 1
}

// hunkLines rebuilds hunk i's body.
func (p *plan) hunkLines(i int) []Line {
	at := p.hunkLineIndex(i)
	return p.lines(at+1, at+1+p.hunkRows(i))
}

// buildPrefix fills in the row prefix index. Every run covers at least one row,
// so the prefix is strictly increasing and a binary search always lands on the
// run that owns a row.
func (p *plan) buildPrefix() {
	p.prefix = make([]int32, len(p.ops)+1)
	for i, o := range p.ops {
		p.prefix[i+1] = p.prefix[i] + int32(o.rows())
	}
}

// empty reports whether the plan renders nothing.
func (p *plan) empty() bool { return len(p.ops) == 0 }

// MatchRow is one match in rendered-row coordinates: the row, and the start and
// end column of the match inside that row's rendered text, sign column included.
type MatchRow struct{ Row, Start, End int }

// matches finds term in rendered rows [from, to) by searching the source content
// the rows are rebuilt from, instead of formatting every line and searching
// that: contiguous runs are scanned byte by byte and an offset is mapped back to
// its line, which is what keeps a search over millions of rows cheap.
//
// It reports false for the terms a source scan cannot express: anything that is
// not plain ASCII, and anything that could match the diff sign column. The
// caller then scans rendered lines instead.
func (p *plan) matches(term string, from, to int) ([]MatchRow, bool) {
	if !searchableTerm(term) {
		return nil, false
	}
	if p.old.lines != nil || p.new.lines != nil {
		// Content too large to index by line: there is nothing to map back to.
		return nil, false
	}
	needle := foldLowerBytes(term)

	from, to = max(0, from), min(to, p.rows())
	var out []MatchRow

	// scan searches lines [lineFrom, lineTo) of one side in one byte pass; the
	// row of the first of them is rowBase.
	scan := func(t text, lineFrom, lineTo, rowBase int) {
		if lineFrom >= lineTo || lineFrom < 0 || lineTo > t.n() {
			return
		}
		start, end := int(t.offs[lineFrom]), t.lineEnd(lineTo-1)
		for i := start; i+len(needle) <= end; {
			j := indexFoldRange(t.s, i, end, needle)
			if j < 0 {
				return
			}
			if strings.IndexByte(t.s[j:j+len(needle)], '\n') < 0 {
				line := t.lineAt(j)
				col := j - int(t.offs[line])
				out = append(out, MatchRow{
					Row:   rowBase + (line - lineFrom),
					Start: col + 1,
					End:   col + 1 + len(needle),
				})
			}
			i = j + len(needle)
		}
	}

	for i := from; i < to; {
		ei := p.entry(i)
		o := p.ops[ei]
		base := int(p.prefix[ei])
		off, left := i-base, to-i

		switch o.kind {
		case opHeader:
			for _, r := range findAllFoldRows(p.headers[o.oldStart], term) {
				out = append(out, MatchRow{Row: i, Start: r[0], End: r[1]})
			}
			i++

		case opEqual:
			n := min(int(o.oldLen)-off, left)
			scan(p.old, int(o.oldStart)+off, int(o.oldStart)+off+n, base)
			i += n

		case opDelete:
			n := min(int(o.oldLen)-off, left)
			scan(p.old, int(o.oldStart)+off, int(o.oldStart)+off+n, base)
			i += n

		case opInsert:
			n := min(int(o.newLen)-off, left)
			scan(p.new, int(o.newStart)+off, int(o.newStart)+off+n, base)
			i += n

		case opReplace:
			body := min(left, o.rows()-off)
			delOff, addOff := off, 0
			if off > int(o.oldLen) {
				delOff, addOff = int(o.oldLen), off-int(o.oldLen)
			}
			nd := min(int(o.oldLen)-delOff, body)
			scan(p.old, int(o.oldStart)+delOff, int(o.oldStart)+delOff+nd, base)
			scan(p.new, int(o.newStart)+addOff, int(o.newStart)+addOff+body-nd, base+int(o.oldLen))
			i += body
		}
	}
	return out, true
}

// searchableTerm reports whether a source scan can find the term: it must be
// plain ASCII and must not be able to match the diff sign column, which only
// exists in the rendered text.
func searchableTerm(term string) bool {
	if term == "" {
		return false
	}
	switch term[0] {
	case ' ', '+', '-':
		return false
	}
	for i := 0; i < len(term); i++ {
		if term[i] >= 0x80 {
			return false
		}
	}
	return true
}

// foldLowerBytes returns the ASCII-lowercased bytes of s.
func foldLowerBytes(s string) []byte {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

// indexFoldRange finds lower (already lowercase) in s[from:to], ignoring ASCII
// case. Gating on the first byte keeps the scan close to memchr speed.
func indexFoldRange(s string, from, to int, lower []byte) int {
	n := len(lower)
	if n == 0 || to-from < n {
		return -1
	}
	first := lower[0]
	for i := from; i+n <= to; i++ {
		c := s[i]
		if c != first {
			if c < 'A' || c > 'Z' || c+('a'-'A') != first {
				continue
			}
		}
		if foldEqual(s[i:i+n], lower) {
			return i
		}
	}
	return -1
}

// foldEqual reports whether s folded equals lower.
func foldEqual(s string, lower []byte) bool {
	if len(s) != len(lower) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[i] {
			return false
		}
	}
	return true
}

// findAllFoldRows finds every occurrence of term in s, ignoring ASCII case.
func findAllFoldRows(s, term string) [][2]int {
	if term == "" {
		return nil
	}
	lower := foldLowerBytes(term)
	var out [][2]int
	for i := 0; i < len(s); {
		j := indexFoldRange(s, i, len(s), lower)
		if j < 0 {
			return out
		}
		out = append(out, [2]int{j, j + len(term)})
		i = j + len(term)
	}
	return out
}
