package diffview

// A blockDoc is a virtual diff. Every block's layout is known as soon as its lines
// are: rows per mode, hunk positions, widest line. The rows themselves are built
// only for a window of blocks around the viewport, so a document of thousands of
// files costs a few ints per block instead of a row per line.
//
// Two shapes share the machinery: file blocks, one per changed file, whose lines
// come back through a loader; and line blocks, one file cut into fixed ranges,
// whose lines are always resident. Blocks are only ever appended, so every
// existing row offset stays stable.

import (
	"fmt"
	"sort"
	"sync"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
)

const (
	// docWindow is how many blocks keep their rows materialised: a handful keeps
	// navigation instant while bounding memory.
	docWindow = 4

	// docBlockLines is how many lines one block of a large file holds.
	docBlockLines = 2048

	modeUnified = 0
	modeSplit   = 1
)

// rowIndex is a Fenwick tree over the per-block row counts, so appending a block
// and asking which block owns a row are both O(log n).
type rowIndex struct {
	rows []int
	tree []int
}

// append records the row count of the next block. The tree keeps a spare half
// because a Fenwick update walks past the entry it starts at.
func (r *rowIndex) append(rows int) {
	r.rows = append(r.rows, rows)
	if len(r.tree) < len(r.rows)+1 {
		r.rebuild()
		return
	}
	r.add(len(r.rows), rows)
}

// add updates the tree at the 1-based position i.
func (r *rowIndex) add(i, delta int) {
	for ; i < len(r.tree); i += i & -i {
		r.tree[i] += delta
	}
}

// reset drops every entry, so a layout whose block sizes changed can be indexed
// again from the start.
func (r *rowIndex) reset() {
	r.rows = r.rows[:0]
	r.tree = r.tree[:0]
}

func (r *rowIndex) rebuild() {
	// Twice the rows it indexes: the spare half keeps the next appends free.
	size := max(16, 2*(len(r.rows)+1))
	if cap(r.tree) < size {
		r.tree = make([]int, size)
	} else {
		r.tree = r.tree[:size]
		clear(r.tree)
	}
	for i, v := range r.rows {
		r.add(i+1, v)
	}
}

// prefix is the number of rows in the first i blocks.
func (r *rowIndex) prefix(i int) int {
	sum := 0
	for ; i > 0; i -= i & -i {
		sum += r.tree[i]
	}
	return sum
}

// count is the total number of rows.
func (r *rowIndex) count() int { return r.prefix(len(r.rows)) }

// find returns the block containing row target and the offset inside it.
func (r *rowIndex) find(target int) (block, offset int) {
	pos, sum := 0, 0
	bit := 1
	for bit<<1 < len(r.tree) {
		bit <<= 1
	}
	for ; bit > 0; bit >>= 1 {
		next := pos + bit
		if next < len(r.tree) && sum+r.tree[next] <= target {
			pos = next
			sum += r.tree[next]
		}
	}
	return pos, target - sum
}

// docBlock is one file, or one line range, in a virtual document.
type docBlock struct {
	path      string
	lineStart int // offset of the block's first line in the document
	lineCount int
	first     int // offset of the block's first line in its source

	rows   [2]int   // rendered rows per mode
	hunks  [2][]int // row offsets of hunk headers inside the block
	widest int
	adds   int // changed lines the block adds, for the document totals
	dels   int // and the lines it removes

	sep   *diff.Line  // optional "── path" band row
	tag   string      // what the band appends to name its side, if anything
	lines []diff.Line // line blocks of a resident file: the lines themselves
	file  *diff.File  // file blocks: the diff to recover lines from

	built [2][]contentRow
}

// blockDoc owns the blocks, the row index and the materialisation window.
type blockDoc struct {
	mu     sync.Mutex
	load   func(i int) *diff.File
	src    *diff.File   // non-nil for a single file split into line blocks
	pre    []contentRow // rows before the first block (notes, file header)
	preTxt []string     // searchable text of those rows
	base   int          // flat index of the document's first line
	blocks []docBlock
	idx    [2]rowIndex
	mode   int
	lines  int
	widest int   // widest rendered line
	preW   int   // widest row above the document
	gutter int   // width of the stacked-row gutter, set by the model
	numW   int   // width of one split column's number gutter
	order  []int // materialised blocks, least recently used first
	loads  int   // materialisations, exposed for tests
}

func newBlockDoc(load func(int) *diff.File) *blockDoc {
	return &blockDoc{load: load}
}

// newLineDoc splits one file's diff into fixed-size blocks.
func newLineDoc(f *diff.File, pre []contentRow, preTxt []string, blockLines int) *blockDoc {
	d := &blockDoc{src: f, pre: pre, preTxt: preTxt}
	d.lines = len(pre) // document lines start after the notes and header
	d.measurePre()

	n := f.NLines()
	for start := 0; start < n; {
		end := blockEnd(f, start, blockLines)
		b := docBlock{
			path:      f.Path(),
			lineStart: d.lines,
			lineCount: end - start,
			first:     start,
		}
		if !f.Paged() {
			// A resident file hands out sub-slices, so a block costs nothing
			// until its rows are built.
			b.lines = f.Lines[start:end]
		}
		d.appendBlock(&b)
		start = end
	}
	d.evict()
	return d
}

// blockEnd is where the block starting at start ends. A change run is never cut in
// half, since split mode pairs deletions with additions.
func blockEnd(f *diff.File, start, blockLines int) int {
	n := f.NLines()
	end := min(start+blockLines, n)
	for end < n && end < start+2*blockLines && f.ChangeAt(end-1) && f.ChangeAt(end) {
		end++
	}
	return end
}

// add ingests the next files, one block each; tags carries each band's label.
func (d *blockDoc) add(files []*diff.File, base int, tags []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.base = base
	for i, f := range files {
		if f == nil {
			continue
		}
		tag := ""
		if i < len(tags) {
			tag = tags[i]
		}
		sep := docSeparator(f, tag)
		b := docBlock{
			path:      f.Path(),
			lineStart: d.lines,
			lineCount: 1 + f.NLines(), // the "── path" band plus the lines
			sep:       &sep,
			tag:       tag,
			file:      f,
			adds:      f.Additions,
			dels:      f.Deletions,
		}
		d.appendBlock(&b)
	}
	d.evict()
}

// replace swaps in new diffs for blocks already laid out, so a live change updates
// the files it touched. Every other block keeps its rows.
func (d *blockDoc) replace(files map[int]*diff.File) bool {
	if d.src != nil || len(files) == 0 {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	changed := false
	for i, f := range files {
		if i < 0 || i >= len(d.blocks) || f == nil {
			continue
		}
		b := &d.blocks[i]
		b.file = f
		b.adds, b.dels = f.Additions, f.Deletions
		b.lineCount = 1 + f.NLines()
		sep := docSeparator(f, b.tag)
		b.sep = &sep
		b.built = [2][]contentRow{}
		// The replaced block left the window.
		for j, v := range d.order {
			if v == i {
				d.order = append(d.order[:j], d.order[j+1:]...)
				break
			}
		}
		b.rows[modeUnified], b.rows[modeSplit], b.hunks[modeUnified], b.hunks[modeSplit], b.widest = blockMetrics(b, d.scanLines(b))
		changed = true
	}
	if !changed {
		return false
	}
	d.relayout()
	d.evict()
	return true
}

// counts sums the changed lines the document holds, from each block's own numbers.
func (d *blockDoc) counts() (adds, dels int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.blocks {
		adds += d.blocks[i].adds
		dels += d.blocks[i].dels
	}
	return adds, dels
}

// fileAt is the diff a block is showing, or nil.
func (d *blockDoc) fileAt(i int) *diff.File {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i < 0 || i >= len(d.blocks) {
		return nil
	}
	return d.blocks[i].file
}

// relayout recomputes what derives from the block line counts: starts, totals and
// the row index. It walks blocks, never rows.
func (d *blockDoc) relayout() {
	d.idx[modeUnified].reset()
	d.idx[modeSplit].reset()
	d.lines = d.rowOffset()
	d.widest = d.preW
	for i := range d.blocks {
		b := &d.blocks[i]
		b.lineStart = d.lines
		d.lines += b.lineCount
		d.widest = max(d.widest, b.widest)
		d.idx[modeUnified].append(b.rows[modeUnified])
		d.idx[modeSplit].append(b.rows[modeSplit])
	}
}

// appendBlock records one block's layout without building its rows.
func (d *blockDoc) appendBlock(b *docBlock) {
	if b.sep != nil {
		b.widest = ansi.StringWidth(b.sep.Text)
	}
	b.rows[modeUnified], b.rows[modeSplit], b.hunks[modeUnified], b.hunks[modeSplit], b.widest = blockMetrics(b, d.scanLines(b))
	d.widest = max(d.widest, b.widest)

	d.blocks = append(d.blocks, *b)
	d.idx[modeUnified].append(b.rows[modeUnified])
	d.idx[modeSplit].append(b.rows[modeSplit])
	d.lines += b.lineCount
	d.order = append(d.order, len(d.blocks)-1)
}

// blockMetrics lays out a block without materialising its rows, and must agree with
// docRows. The lines arrive through scan, so a paged block lays out without
// allocating a line.
func blockMetrics(b *docBlock, scan func(func(kind diff.LineKind, width int))) (rowsU, rowsS int, hunksU, hunksS []int, widest int) {
	widest = b.widest
	offU, offS := 0, 0
	if b.sep != nil {
		offU, offS = 1, 1
	}

	n, dels, adds := 0, 0, 0
	flush := func() {
		offS += max(dels, adds)
		dels, adds = 0, 0
	}
	scan(func(kind diff.LineKind, width int) {
		widest = max(widest, width)
		if kind == diff.HunkHeader {
			hunksU = append(hunksU, offU+n)
		}
		n++

		switch kind {
		case diff.Deletion:
			dels++
		case diff.Addition:
			adds++
		default:
			flush()
			if kind == diff.HunkHeader {
				hunksS = append(hunksS, offS)
			}
			offS++
		}
	})
	flush()

	return offU + n, offS, hunksU, hunksS, widest
}

// scanLines walks a block's lines in order, without building them.
func (d *blockDoc) scanLines(b *docBlock) func(func(kind diff.LineKind, width int)) {
	return func(yield func(diff.LineKind, int)) {
		if b.lines != nil {
			for _, l := range b.lines {
				yield(l.Kind, len(l.Text))
			}
			return
		}
		if f := d.source(b); f != nil {
			f.ScanLines(b.first, b.first+b.lineCount, yield)
		}
	}
}

// source is the diff a block's lines come from.
func (d *blockDoc) source(b *docBlock) *diff.File {
	if b.file != nil {
		return b.file
	}
	return d.src
}

// fileForRow returns the diff behind document row i, which picks the language for
// its tokens. Only the all-changes view mixes files.
func (d *blockDoc) fileForRow(i int) *diff.File {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil {
		return d.src
	}
	if len(d.blocks) == 0 {
		return nil
	}
	bi, _ := d.idx[d.mode].find(max(0, i-d.rowOffset()))
	if bi < 0 || bi >= len(d.blocks) {
		return nil
	}
	return d.source(&d.blocks[bi])
}

// blockLines returns a block's lines, rebuilt from its source when not resident.
func (d *blockDoc) blockLines(b *docBlock) []diff.Line {
	if b.lines != nil {
		return b.lines
	}
	f := d.source(b)
	if f == nil {
		return nil
	}
	return f.LinesRange(b.first, b.first+b.lineCount)
}

// evict drops the rows of blocks outside the window, keeping their layout.
func (d *blockDoc) evict() {
	for len(d.order) > docWindow {
		i := d.order[0]
		d.order = d.order[1:]
		d.blocks[i].built = [2][]contentRow{}
		if d.src == nil {
			// File blocks recover their lines through the loader.
			d.blocks[i].file = nil
		}
	}
}

// materialise makes sure block i has rows, marking it recently used.
func (d *blockDoc) materialise(i int) *docBlock {
	if i < 0 || i >= len(d.blocks) {
		return nil
	}
	b := &d.blocks[i]

	if b.built[d.mode] == nil {
		var sep *diff.Line
		if d.src == nil {
			if b.file == nil {
				d.loads++
				if d.load != nil {
					b.file = d.load(i)
				}
			}
			if b.file == nil {
				return nil
			}
			sep = b.sep
		}
		lines := d.blockLines(b)
		b.built[d.mode] = docRows(lines, d.base+b.lineStart, d.mode == modeSplit, sep)
	}

	for j, v := range d.order {
		if v == i {
			d.order = append(d.order[:j], d.order[j+1:]...)
			break
		}
	}
	d.order = append(d.order, i)
	d.evict()
	return b
}

// setMode switches the layout between unified and split rendering.
func (d *blockDoc) setMode(split bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if split {
		d.mode = modeSplit
	} else {
		d.mode = modeUnified
	}
}

// setPre replaces the rows rendered above the document, which only a single-file
// document has; their number is fixed, so line offsets stay valid.
func (d *blockDoc) setPre(pre []contentRow, preTxt []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(pre) != len(d.pre) {
		return
	}
	d.pre = pre
	d.preTxt = preTxt
	d.measurePre()
}

// measurePre records the width of the rows above the document.
func (d *blockDoc) measurePre() {
	d.preW = 0
	for _, r := range d.pre {
		d.preW = max(d.preW, ansi.StringWidth(r.text))
	}
}

// rowOffset is how many rows of the document come before the first block.
func (d *blockDoc) rowOffset() int {
	if d.src != nil {
		return len(d.pre)
	}
	return 0
}

// single reports whether the document is one file split into line blocks.
func (d *blockDoc) single() bool { return d.src != nil }

// flatBase is the flat index of the document's first line. File blocks are
// offset by the notes and header rows that a flat aggregate would have had;
// line blocks are numbered in the same space as the flat document already.
func (d *blockDoc) flatBase() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.base
}

// count is the number of rendered rows in the active mode.
func (d *blockDoc) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rowOffset() + d.idx[d.mode].count()
}

// nlines is the number of document lines, which is what search indexes.
func (d *blockDoc) nlines() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lines
}

func (d *blockDoc) nblocks() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.blocks)
}

// row returns row i of the active mode, materialising its block if needed.
func (d *blockDoc) row(i int) contentRow {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil && i >= 0 && i < len(d.pre) {
		return d.pre[i]
	}
	if len(d.blocks) == 0 {
		return contentRow{}
	}
	bi, off := d.idx[d.mode].find(max(0, i-d.rowOffset()))
	b := d.materialise(bi)
	if b == nil {
		return contentRow{}
	}
	rows := b.built[d.mode]
	if off < 0 || off >= len(rows) {
		return contentRow{}
	}
	return rows[off]
}

// lineForRow returns the diff line behind a unified row, for the gutter.
func (d *blockDoc) lineForRow(i int) (diff.Line, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil && i >= 0 && i < len(d.pre) {
		return diff.Line{}, false
	}
	if len(d.blocks) == 0 {
		return diff.Line{}, false
	}
	bi, off := d.idx[modeUnified].find(max(0, i-d.rowOffset()))
	b := d.materialise(bi)
	if b == nil {
		return diff.Line{}, false
	}
	rows := b.built[modeUnified]
	if off < 0 || off >= len(rows) {
		return diff.Line{}, false
	}
	return rows[off].line, true
}

// textAt returns the searchable text of document line i.
func (d *blockDoc) textAt(line int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if line < 0 {
		return ""
	}
	if d.src != nil && line < len(d.preTxt) {
		return d.preTxt[line]
	}
	if len(d.blocks) == 0 {
		return ""
	}

	i := sort.Search(len(d.blocks), func(k int) bool { return d.blocks[k].lineStart > line }) - 1
	if i < 0 {
		return ""
	}
	b := &d.blocks[i]
	local := line - b.lineStart
	if b.sep != nil {
		if local == 0 {
			return b.sep.Text
		}
		local--
	}
	if local < 0 || local >= b.lineCount {
		return ""
	}
	if b.file == nil && d.src == nil {
		// A file block that was evicted needs its diff back to be searchable.
		b = d.materialise(i)
		if b == nil {
			return ""
		}
	}
	f := d.source(b)
	if f == nil {
		return ""
	}
	return f.LineText(b.first + local)
}

// lineRange is the span of lines covered by the rows around yoff, so the first
// search phase can scan only what is on screen.
func (d *blockDoc) lineRange(yoff, height int) (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	idx := &d.idx[d.mode]
	if len(d.blocks) == 0 || idx.count() == 0 {
		return 0, 0
	}
	last := idx.count() - 1
	from, _ := idx.find(min(max(0, yoff-d.rowOffset()), last))
	to, _ := idx.find(min(max(0, yoff+max(1, height)-d.rowOffset()), last))
	end := d.blocks[to].lineStart + d.blocks[to].lineCount
	return d.blocks[from].lineStart, end
}

// lineCount is the document's total line count.
func (d *blockDoc) lineCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lines
}

// preCount is how many rows sit above the first block, so a caller can tell
// whether appending moved every block down.
func (d *blockDoc) preCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pre)
}

// setGutter records the gutter widths the model is using: the whole gutter for
// stacked rows and one side's number column for split columns.
func (d *blockDoc) setGutter(cells, numW int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.gutter, d.numW = cells, numW
}

// width reports the document's content width, mirroring Model.rowWidth.
func (d *blockDoc) width(vpWidth int, split, numbers bool) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if split {
		colW := vpWidth / 2
		gut := 0
		if numbers {
			gut = d.numW
		}
		contentW := max(1, colW-gut)
		w := max(d.preW, vpWidth)
		if d.widest > contentW {
			w = max(w, vpWidth+(d.widest-contentW))
		}
		return w
	}
	gut := 0
	if numbers {
		gut = d.gutter
	}
	return max(d.preW, gut+d.widest)
}

// layout returns block i's first row and its hunk rows, in document
// coordinates for the active mode.
func (d *blockDoc) layout(i int) (int, []int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i < 0 || i >= len(d.blocks) {
		return 0, nil
	}
	return d.rowOffset() + d.idx[d.mode].prefix(i), d.blocks[i].hunks[d.mode]
}

// rowForLine maps a document line onto the row that paints it, so a scroll
// target survives a rebuild. Split mode collapses pairs, so the row is clamped
// inside the block.
func (d *blockDoc) rowForLine(line int) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil && line < len(d.pre) {
		return line
	}
	if len(d.blocks) == 0 {
		return 0
	}
	i := sort.Search(len(d.blocks), func(k int) bool { return d.blocks[k].lineStart > line }) - 1
	if i < 0 {
		i = 0
	}
	b := &d.blocks[i]
	local := min(max(line-b.lineStart, 0), b.lineCount-1)

	rows := b.rows[d.mode]
	local = min(local, max(0, rows-1))
	return d.rowOffset() + d.idx[d.mode].prefix(i) + local
}

// lineAtRow is the inverse of rowForLine: the line painted by a row, which is
// what keeps a reader in place when the document is rebuilt under them.
func (d *blockDoc) lineAtRow(row int) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	row = max(0, row-d.rowOffset())
	if len(d.blocks) == 0 {
		return 0
	}
	if d.src != nil && row < len(d.pre) {
		return row
	}
	i, offset := d.idx[d.mode].find(row)
	if i < 0 || i >= len(d.blocks) {
		return 0
	}
	b := &d.blocks[i]
	return b.lineStart + min(offset, max(0, b.lineCount-1))
}

// patchAt renders the raw patch of the global hunk idx. File blocks keep the
// block and hunk-local indexes that address it.
func (d *blockDoc) patchAt(idx, block, local int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil {
		return d.src.HunkPatch(idx)
	}

	b := d.materialise(block)
	if b == nil || b.file == nil {
		return ""
	}
	return b.file.HunkPatch(local)
}

// blockPatchAt renders the whole patch of the block containing row i.
func (d *blockDoc) blockPatchAt(row int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.src != nil {
		return d.src.Patch()
	}
	if len(d.blocks) == 0 {
		return ""
	}
	bi, _ := d.idx[d.mode].find(max(0, row-d.rowOffset()))
	b := d.materialise(bi)
	if b == nil || b.file == nil {
		return ""
	}
	return b.file.Patch()
}

// materialised reports how many blocks currently hold rows, for tests.
func (d *blockDoc) materialised() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for i := range d.blocks {
		if d.blocks[i].built[d.mode] != nil {
			n++
		}
	}
	return n
}

// docRows lays out one block: an optional band row, then the block's rows for
// the given rendering mode. flatBase is the flat index of the block's first
// line.
func docRows(lines []diff.Line, flatBase int, split bool, sep *diff.Line) []contentRow {
	n := len(lines)
	if sep != nil {
		n++
	}
	rows := make([]contentRow, 0, n)

	off := flatBase
	if sep != nil {
		rows = append(rows, contentRow{fullWidth: true, line: *sep, flat: off, hunk: true})
		off++
	}
	if split {
		return append(rows, splitRows(lines, off)...)
	}
	for i, l := range lines {
		rows = append(rows, contentRow{
			fullWidth: true,
			line:      l,
			flat:      off + i,
			hunk:      l.Kind == diff.HunkHeader,
		})
	}
	return rows
}

// docSeparator is the "── path  +a -d" band line every file block starts with.
// tag, when set, names which half of the file the block shows: a partly staged
// file is in the document twice, and two bands with the same text say nothing
// about which one a keystroke would act on.
func docSeparator(f *diff.File, tag string) diff.Line {
	text := "── " + f.Path() + tag
	if f.Binary {
		text += "  binary"
	} else {
		text += fmt.Sprintf("  +%d -%d", f.Additions, f.Deletions)
	}
	return diff.Line{Kind: diff.HunkHeader, Text: text}
}
