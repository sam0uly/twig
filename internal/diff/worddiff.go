package diff

import "time"

// Word-level (intra-line) diff, in the spirit of Zed and VS Code's diff
// views: adjacent deletion/addition runs are compared token by token and the
// tokens that actually changed are marked, so a modified line highlights
// only the words that differ rather than the entire line.
//
// Deletion rows are paired with addition rows by token similarity
// (delta-style homologous pairing) instead of by position, so a rewritten run
// that shifted still highlights the right line. Each pair goes through the
// same interning + Myers engine as the line diff, so reordered tokens are
// matched rather than reported as one big change.

// Span is a byte range in a line's Text, [start, end).
type Span [2]int

// WordRanges returns the changed-word ranges for a line, as byte offsets
// into Text. Only lines that were part of a word-diff comparison carry
// ranges.
func (l Line) WordRanges() []Span { return l.words }

// token is a whitespace or word token with its byte range in the source.
// token is one run of a line: whitespace, a word, or a run of punctuation.
// Words are split from punctuation so that a change inside
// printSuccess("added") points at the word, not at the whole call.
type token struct {
	start, end int
	isSpace    bool
	str        string
}

func tokenize(s string) []token {
	var toks []token
	for i := 0; i < len(s); {
		j := i
		switch {
		case isSpaceByte(s[i]):
			for j < len(s) && isSpaceByte(s[j]) {
				j++
			}
		case isWordByte(s[i]):
			for j < len(s) && isWordByte(s[j]) {
				j++
			}
		default:
			for j < len(s) && !isSpaceByte(s[j]) && !isWordByte(s[j]) {
				j++
			}
		}
		toks = append(toks, token{start: i, end: j, isSpace: isSpaceByte(s[i]), str: s[i:j]})
		i = j
	}
	return toks
}

// isWordByte reports whether c belongs to a word: identifier bytes, digits and
// the bytes that continue a multi-byte character, so a non-ASCII letter stays
// part of its word.
func isWordByte(c byte) bool {
	switch {
	case c == '_' || c == '$':
		return true
	case c >= '0' && c <= '9':
		return true
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	default:
		return c >= 0x80
	}
}

func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

const (
	// homologyWindow is how many addition rows a deletion may look ahead for
	// its best partner.
	homologyWindow = 16
	// homologyFloor rejects pairings that share too little to be a rewrite
	// of the same line; unpaired lines simply keep their plain wash.
	homologyFloor = 0.25

	// maxPairRows is how long a change run may be before word highlighting is
	// skipped. Past it the run is a rewrite, where pairing costs O(rows*window)
	// and tells the reader nothing.
	maxPairRows = 2000

	// maxPairCells is the largest deletion-by-addition block that gets an
	// optimal alignment instead of the greedy pass. A 64 by 64 block is 4096
	// cells, which costs microseconds and covers every ordinary hunk.
	maxPairCells = 4096

	// pairDisplacement is how much a pair loses for sitting off the
	// proportional diagonal of its block. Prose lines resemble each other
	// closely, so similarity alone lets the alignment drift sideways. The
	// displacement is always below one, so the penalty is always smaller than
	// this value: it can only break pairs already within 0.1 of each other,
	// never override a real similarity margin.
	pairDisplacement = 0.1

	// maxWordLine is the longest line worth tokenizing. A minified file would
	// otherwise spend the whole budget on a handful of lines.
	maxWordLine = 1024

	// wordBudget bounds one annotation pass, the way editors bound their
	// inline diff: highlighting is a refinement, never the thing that decides
	// when a page appears.
	wordBudget = 150 * time.Millisecond
)

// annotateWordDiff marks changed-word spans on the flat lines of a parsed or
// resident diff, pairing deletion and addition runs by token similarity. Hunk
// headers naturally separate runs, so blocks never span hunks.
func annotateWordDiff(f *File) {
	if f.Binary || f.page != nil {
		// A paged diff is annotated by the plan as its pages are built.
		return
	}
	annotateLines(f.Lines, false, false)
}

// annotateLines marks the changed words of the deletion and addition runs in
// lines, within the default budget.
func annotateLines(lines []Line, cutStart, cutEnd bool) {
	annotateWithin(lines, cutStart, cutEnd, time.Now().Add(wordBudget))
}

// annotateWithin marks the changed words of every hunk in lines. cutStart and
// cutEnd say the slice is a page cut out of a larger body: a hunk touching a
// cut edge is left plain, because part of it may lie outside the page. The
// deadline stops the pass mid-document: the remaining hunks keep their plain
// wash.
//
// The unit is the hunk, not the run: an edit script can interleave deletions
// and additions, and pairing inside a single run would then compare unrelated
// lines. A hunk holds the whole rewritten block, which is what the reader sees.
func annotateWithin(lines []Line, cutStart, cutEnd bool, deadline time.Time) {
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && lines[i].Kind != HunkHeader {
			continue
		}
		if i > start {
			cutByStart := cutStart && start == 0
			cutByEnd := cutEnd && i == len(lines)
			if !cutByStart && !cutByEnd {
				if !time.Now().Before(deadline) {
					return
				}
				annotateHunk(lines[start:i], deadline)
			}
		}
		start = i + 1
	}
}

// block is one hunk's changed rows: every deletion and every addition it
// contains, in row order.
type block struct{ dels, adds []int }

// changeBlocks returns the hunk's changed rows as one block. Deletions and
// additions are collected across the whole hunk rather than in contiguous runs,
// because the edit script can interleave them with unchanged rows: a swap emits
// both deletions, an unchanged row, then both additions.
func changeBlocks(part []Line) []block {
	var b block
	for i := range part {
		switch part[i].Kind {
		case Deletion:
			b.dels = append(b.dels, i)
		case Addition:
			b.adds = append(b.adds, i)
		}
	}
	if len(b.dels) == 0 && len(b.adds) == 0 {
		return nil
	}
	return []block{b}
}

// isChangeRow reports whether a row is part of a change rather than context.
func isChangeRow(k LineKind) bool { return k == Deletion || k == Addition }

// annotateHunk pairs the changed rows of one hunk.
func annotateHunk(part []Line, deadline time.Time) {
	for _, b := range changeBlocks(part) {
		annotateBlock(part, b, deadline)
	}
}

// annotateBlock pairs one block's deletions with its additions.
func annotateBlock(part []Line, b block, deadline time.Time) {
	if !worthPairing(part, b.dels, b.adds) {
		return
	}
	for n, p := range pairRows(part, b.dels, b.adds) {
		if n%64 == 0 && !time.Now().Before(deadline) {
			return
		}
		del, add := &part[p[0]], &part[p[1]]
		oldSpans, newSpans := changedSpans(del.Text[1:], add.Text[1:])
		// Store offsets against the full line text (sign included).
		for _, s := range oldSpans {
			del.words = append(del.words, Span{s[0] + 1, s[1] + 1})
		}
		for _, s := range newSpans {
			add.words = append(add.words, Span{s[0] + 1, s[1] + 1})
		}
	}
}

// worthPairing rejects the hunks where word highlighting is pure cost: a long
// rewrite, or lines so long that tokenizing them would dominate the pass.
func worthPairing(part []Line, dels, adds []int) bool {
	if len(dels) == 0 || len(adds) == 0 {
		return false
	}
	if len(dels) > maxPairRows || len(adds) > maxPairRows {
		return false
	}
	for _, i := range dels {
		if len(part[i].Text) > maxWordLine {
			return false
		}
	}
	for _, i := range adds {
		if len(part[i].Text) > maxWordLine {
			return false
		}
	}
	return true
}

// pairRows pairs the deletion rows with the addition rows of one hunk by token
// similarity, preserving order. Each row is used at most once.
//
// A small block gets an optimal alignment, because a greedy pass can walk past
// a row's real partner and leave it unpaired: a paragraph rewritten line by
// line would then lose its word highlights. Large blocks fall back to the
// greedy windowed pass, which is linear in the block size.
func pairRows(lines []Line, dels, adds []int) [][2]int {
	return pairRowsWith(lines, dels, adds, pairDisplacement)
}

// pairRowsWith is pairRows with the displacement weight spelled out, so a test
// can compare the alignment against pure similarity.
func pairRowsWith(lines []Line, dels, adds []int, disp float64) [][2]int {
	nDel, nAdd := len(dels), len(adds)
	if nDel == 0 || nAdd == 0 {
		return nil
	}

	in := make(interner, 64)
	bags := make([]map[int32]int, 0, nDel+nAdd)
	for _, i := range dels {
		bags = append(bags, wordBag(lines[i].Text[1:], in))
	}
	for _, i := range adds {
		bags = append(bags, wordBag(lines[i].Text[1:], in))
	}

	// The cell cap only decides how much work the alignment may do: both
	// paths score a pair the same way.
	if nDel*nAdd <= maxPairCells {
		return pairRowsOptimal(bags, nDel, nAdd, dels, adds, disp)
	}
	return pairRowsGreedy(bags, nDel, nAdd, dels, adds, disp)
}

// pairScore is what one candidate pair is worth: its similarity, minus a
// penalty for sitting off the block's proportional diagonal. The displacement
// is below one, so the penalty is always below disp and can only break pairs
// already within disp of each other.
func pairScore(bags []map[int32]int, nDel, nAdd, i, j int, disp float64) float64 {
	s := bagSimilarity(bags[i], bags[nDel+j])
	if s < homologyFloor {
		return 0
	}
	d := float64(i)/float64(nDel) - float64(j)/float64(nAdd)
	if d < 0 {
		d = -d
	}
	// A pair has to earn back twice the floor. Charging only the floor leaves
	// it cheaper to add two weak pairs than to leave one good one alone, which
	// is how unrelated rows end up paired.
	return s - 2*homologyFloor - disp*d
}

// pairRowsOptimal is a maximum-weight monotonic alignment of the two rows: a
// pair contributes its score, a gap contributes nothing, so rows keep their
// order and every pairing above the floor is available.
func pairRowsOptimal(bags []map[int32]int, nDel, nAdd int, dels, adds []int, disp float64) [][2]int {
	w := nAdd + 1
	dp := make([]float64, (nDel+1)*w)
	for i := 1; i <= nDel; i++ {
		for j := 1; j <= nAdd; j++ {
			best := dp[(i-1)*w+j]
			if v := dp[i*w+j-1]; v > best {
				best = v
			}
			if v := dp[(i-1)*w+j-1] + pairScore(bags, nDel, nAdd, i-1, j-1, disp); v > best {
				best = v
			}
			dp[i*w+j] = best
		}
	}

	var rev [][2]int
	for i, j := nDel, nAdd; i > 0 && j > 0; {
		if s := pairScore(bags, nDel, nAdd, i-1, j-1, disp); s > 0 && dp[(i-1)*w+j-1]+s == dp[i*w+j] {
			rev = append(rev, [2]int{dels[i-1], adds[j-1]})
			i--
			j--
			continue
		}
		if dp[(i-1)*w+j] == dp[i*w+j] {
			i--
			continue
		}
		j--
	}

	pairs := make([][2]int, 0, len(rev))
	for k := len(rev) - 1; k >= 0; k-- {
		pairs = append(pairs, rev[k])
	}
	return pairs
}

// pairRowsGreedy is the bounded fallback: each deletion takes its best unused
// partner inside a look-ahead window, and the cursor only moves forward. It
// scores pairs exactly like the optimal path, so the cell cap costs quality,
// not consistency.
func pairRowsGreedy(bags []map[int32]int, nDel, nAdd int, dels, adds []int, disp float64) [][2]int {
	used := make([]bool, nAdd)
	last := 0
	var pairs [][2]int
	for i := 0; i < nDel; i++ {
		best, bestScore := -1, 0.0
		limit := min(nAdd, last+homologyWindow)
		for a := last; a < limit; a++ {
			if used[a] {
				continue
			}
			if s := pairScore(bags, nDel, nAdd, i, a, disp); s > bestScore {
				best, bestScore = a, s
			}
		}
		if best < 0 {
			continue
		}
		pairs = append(pairs, [2]int{dels[i], adds[best]})
		used[best] = true
		last = best + 1
	}
	return pairs
}

// interner maps token text to one id for a whole block. Sharing it is what
// makes two bags comparable: ids interned per line would compare a line's
// token positions instead of its tokens.
type interner map[string]int32

// id returns the id for s, assigning the next free one on first use.
func (in interner) id(s string) int32 {
	if id, ok := in[s]; ok {
		return id
	}
	id := int32(len(in))
	in[s] = id
	return id
}

// wordBag counts the non-space tokens of a line, interned through in.
func wordBag(s string, in interner) map[int32]int {
	toks := tokenize(s)
	bag := make(map[int32]int, len(toks))
	for _, t := range toks {
		if t.isSpace {
			continue
		}
		bag[in.id(t.str)]++
	}
	return bag
}

// bagSimilarity is the Dice coefficient of two token multisets.
func bagSimilarity(a, b map[int32]int) float64 {
	total := 0
	for _, n := range a {
		total += n
	}
	for _, n := range b {
		total += n
	}
	if total == 0 {
		return 0
	}
	common := 0
	for id, n := range a {
		if m, ok := b[id]; ok {
			common += min(n, m)
		}
	}
	return 2 * float64(common) / float64(total)
}

// changedSpans returns the byte ranges of tokens that differ between the
// two strings.
func changedSpans(oldText, newText string) (oldSpans, newSpans []Span) {
	if oldText == newText {
		return nil, nil
	}

	ot, nt := tokenize(oldText), tokenize(newText)

	oldStrs := make([]string, len(ot))
	for i, t := range ot {
		oldStrs[i] = t.str
	}
	newStrs := make([]string, len(nt))
	for i, t := range nt {
		newStrs[i] = t.str
	}
	a, b := lineIDs(oldStrs, newStrs)

	oldMatch := make([]bool, len(ot))
	newMatch := make([]bool, len(nt))
	for _, e := range diffIDs(a, b) {
		if e.kind != editEqual {
			continue
		}
		oldMatch[e.oldIdx] = true
		newMatch[e.newIdx] = true
	}
	return spansOf(ot, oldMatch), spansOf(nt, newMatch)
}

// spansOf merges consecutive unmatched tokens into spans of their source
// string.
func spansOf(toks []token, matched []bool) []Span {
	var spans []Span
	start := -1
	for i := range toks {
		if matched[i] {
			if start >= 0 {
				spans = append(spans, Span{toks[start].start, toks[i-1].end})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		spans = append(spans, Span{toks[start].start, toks[len(toks)-1].end})
	}
	return spans
}
