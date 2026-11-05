// Package diffview renders one file's diff as a pager: a full-bleed viewport
// with its own scrollbars, word-level highlights and search matches, unified or
// side by side. A large diff becomes a virtual document, so only the rows around
// the window are built.
package diffview

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/syntax"
	"samouly.fun/twig/internal/ui/styles"
)

const (
	frameInterval = time.Second / 60
	// baseSpringFreq is the stiffness of the scroll spring for a short step, in
	// radians per second; it rises with the distance left, so long jumps arrive fast.
	baseSpringFreq = 26.0
	// maxSpringFreq caps that rise, so a long jump stays quick without becoming a cut.
	maxSpringFreq = 60.0
	// smallStepRows is the distance a scroll may move before gliding is worth it: a
	// line or a wheel tick lands at once, or the view feels behind the keyboard.
	smallStepRows = 3
	// scrollbarSize is the cells reserved at the right and the bottom for scrollbars.
	scrollbarSize = 1
	// maxCachedRows bounds the memoised row cache.
	maxCachedRows = 1 << 16
)

// Match is a search match in rendered content coordinates.
type Match struct {
	Line       int
	Start, End int
}

// gutterState is what the viewport gutter needs to draw a row. The gutter
// callback is captured once, when the viewport is created, so this is shared by
// pointer: a Model copy would never see later changes.
type gutterState struct {
	file  *diff.File
	notes int
	show  bool
	split bool
	// header is 1 when a file header row is drawn above the diff, 0 for a commit
	// document.
	header int
	// numW is the cells reserved per side for a line number.
	numW int
	// lineFor resolves a document row to its diff line, for the virtual document.
	lineFor func(row int) (diff.Line, bool)
}

// contentRow is one rendered row: one line unified, a deleted/added pair in split.
type contentRow struct {
	// pre marks rows whose text is already rendered (notes, file header).
	pre  bool
	text string
	// fullWidth rows span both columns even in split mode (hunk headers, markers).
	fullWidth bool
	line      diff.Line
	flat      int
	hunk      bool

	left, right         diff.Line
	hasLeft, hasRight   bool
	leftFlat, rightFlat int
}

// rowCache memoises styled rows for one mode, so a scroll frame only styles the
// rows entering the window. Shared by pointer, since Model is copied by value.
type rowCache struct {
	rows        map[int]string
	xoff, width int
}

// reset drops every memoised row.
func (c *rowCache) reset() {
	clear(c.rows)
}

// Model renders one file's diff.
type Model struct {
	vp     viewport.Model
	styles styles.Styles
	file   *diff.File
	// doc is the virtual multi-file document, nil when one file is shown.
	doc   *blockDoc
	gut   *gutterState
	cache *rowCache

	notes []string // metadata lines above the diff
	// noteRows is those lines wrapped to the viewport width, notePlain their
	// searchable text.
	noteRows  []string
	notePlain []string
	rows      []contentRow
	indexed   int      // rows already folded into hunks, bands and width
	laid      int      // document lines already laid out as rows
	rendered  []string // ANSI-rendered content lines
	// wrapPrefix[i] is the display line row i starts at when soft wrap is on.
	wrapPrefix []int
	// blank is the row placeholder buffer handed to the viewport, reused across
	// rebuilds.
	blank     []string
	plain     []string // plain text, search coordinates
	hunks     []int    // rendered-line indexes of hunk headers
	fileBands []int    // rendered-line indexes of per-file bands
	// hunkBlock and hunkLocal map a global hunk index back to its block.
	hunkBlock []int32
	hunkLocal []int32
	// hunkCur is the hunk targeted by the last hunk jump, or -1.
	hunkCur int

	contentWidth int // widest rendered line, drives the horizontal scrollbar

	term     string
	matches  []Match
	matchIdx int
	// searchSeq numbers the running search so late results can be dropped.
	searchSeq int
	// folded is an ASCII-lowercased mirror of plain, so search can use strings.Index
	// and match offsets stay valid.
	folded []string

	// cursor is the row the staging cursor sits on, or -1 before it is placed; anchor
	// is the other end of a range, or -1. pending is the mark set select mode applies
	// in one go, markPath the file it belongs to, since a selection names lines and
	// not files. selectable says staging is available here, cursorOn that this pane
	// holds the keys.
	cursor     int
	anchor     int
	pending    *stage.Selection
	markPath   string
	selectable bool
	selectMode bool
	cursorOn   bool
	// fileStaged is which half of the file on screen this pane shows; blockStaged
	// carries one for each block of a document, where a file can appear twice.
	fileStaged  bool
	blockStaged []bool

	smooth  bool
	spring  harmonica.Spring
	pos     float64
	vel     float64
	target  float64
	anim    bool
	animSeq int
	// scrollCmd is the frame command of a glide a cursor move started: a cursor
	// movement has no command to return it in, so the caller takes it here.
	scrollCmd tea.Cmd
	// lastFrame is when the previous frame ran, so the spring advances by real time.
	lastFrame time.Time

	// syn is the token index of each drawn file, built on first paint; syntaxOn gates
	// the feature so the off path is the plain renderer.
	syn      map[rowSource]*syntax.Index
	syntaxOn bool

	split bool
	wrap  bool
	// rx is the compiled regular expression when the term looks like one.
	rx              *regexp.Regexp
	width, height   int
	showLineNumbers bool
}

// scrollFrameMsg drives one animation frame. The sequence number makes stale
// frames from a superseded animation harmless; the timestamp gives the spring the
// real elapsed time.
type scrollFrameMsg struct {
	seq int
	at  time.Time
}

// New builds a diff view.
func New(smooth bool, st styles.Styles) Model {
	gut := &gutterState{show: true, numW: 4}

	vp := viewport.New()
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	vp.FillHeight = true
	// Keep the pager-style bindings, but free "u" for the app's unstage key.
	vp.KeyMap = viewport.DefaultKeyMap()
	vp.KeyMap.HalfPageUp = key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("^u", "½ page up"),
	)
	vp.LeftGutterFunc = func(ctx viewport.GutterContext) string {
		return gutterText(gut, ctx)
	}

	return Model{
		vp:              vp,
		styles:          st,
		gut:             gut,
		cache:           &rowCache{rows: make(map[int]string)},
		syn:             make(map[rowSource]*syntax.Index),
		syntaxOn:        true,
		smooth:          smooth,
		spring:          harmonica.NewSpring(frameInterval.Seconds(), baseSpringFreq, 1.0),
		showLineNumbers: true,
		hunkCur:         -1,
		cursor:          -1,
		anchor:          -1,
		pending:         stage.NewSelection(),
	}
}

// SetStyles updates the theme and re-renders the content.
func (m *Model) SetStyles(st styles.Styles) {
	m.styles = st
	m.rebuild()
}

// SetSyntax turns token colouring on or off and drops the indexes either way.
func (m *Model) SetSyntax(on bool) {
	if m.syntaxOn == on {
		return
	}
	m.syntaxOn = on
	clear(m.syn)
	m.rebuild()
}

// SetSize resizes the viewport; the scrollbars live inside the given box.
func (m *Model) SetSize(width, height int) {
	changed := m.width != width
	m.width, m.height = width, height
	m.vp.SetWidth(m.vpWidth())
	m.vp.SetHeight(m.vpHeight())
	if changed && m.file != nil {
		// Line backgrounds are padded to the viewport width.
		m.rebuild()
	}
}

// bars reports whether the scrollbars are shown.
func (m Model) bars() bool { return !m.vp.SoftWrap && m.width >= 3 && m.height >= 3 }

func (m Model) vpWidth() int {
	w := m.width
	if m.bars() {
		w -= scrollbarSize
	}
	// Wrapped rows carry the staging bar inside them; the unwrapped path paints it
	// outside the row cache and rowBudget accounts for it instead.
	if m.vp.SoftWrap && m.barWidth() > 0 {
		w -= m.barWidth()
	}
	return max(1, w)
}

// rowBudget is the cells a row's content may fill: the viewport minus the staging
// bar that paintRow prepends.
func (m Model) rowBudget() int {
	if m.vp.SoftWrap {
		// vpWidth already took the bar off.
		return m.vpWidth()
	}
	return max(1, m.vpWidth()-m.barWidth())
}

func (m Model) vpHeight() int {
	if !m.bars() {
		return max(1, m.height)
	}
	// The bottom bar only takes a row when there is something to scroll sideways.
	if m.hscroll() {
		return max(1, m.height-scrollbarSize)
	}
	return max(1, m.height)
}

// hscroll reports whether the horizontal scrollbar is needed at all.
func (m Model) hscroll() bool {
	return m.bars() && m.contentWidth > m.rowBudget()
}

// SetWrap toggles soft wrapping, which split mode disables.
func (m *Model) SetWrap(wrap bool) {
	if m.wrap == wrap {
		return
	}
	m.wrap = wrap
	m.applyWrap()
	m.rebuild()
}

func (m *Model) applyWrap() {
	m.vp.SoftWrap = m.wrap && !m.split
	m.vp.SetWidth(m.vpWidth())
	m.vp.SetHeight(m.vpHeight())
}

// SetLineNumbers toggles the line-number gutter.
func (m *Model) SetLineNumbers(show bool) {
	if m.showLineNumbers == show {
		return
	}
	m.showLineNumbers = show
	m.gut.show = show
	m.rebuild()
}

// ShowLineNumbers reports whether line numbers are shown.
func (m Model) ShowLineNumbers() bool { return m.showLineNumbers }

// SetSplit switches between the unified (false) and side-by-side (true)
// rendering of the diff.
func (m *Model) SetSplit(split bool) {
	if m.split == split {
		return
	}
	m.split = split
	m.gut.split = split
	m.applyWrap()
	m.setContent()
}

// ToggleSplit flips between unified and split rendering.
func (m *Model) ToggleSplit() { m.SetSplit(!m.split) }

// Split reports whether the split rendering is active.
func (m Model) Split() bool { return m.split }

// SetFile renders the given file, or clears the view when nil. Notes are
// metadata lines (e.g. commit details) rendered above the diff. A very large
// diff is laid out as a virtual document, so only the rows around the viewport
// are ever materialised.
func (m *Model) SetFile(f *diff.File) {
	if f != nil && f.NLines() > hugeFileLines {
		m.SetLines(f)
		return
	}
	m.setFlat(f)
}

// hugeFileLines is where a single diff switches to the virtual layout.
const hugeFileLines = 50_000

// setFlat lays the diff out as one flat row slice, the reference the virtual
// layout has to match.
func (m *Model) setFlat(f *diff.File) {
	m.doc = nil
	m.gut.lineFor = nil
	m.file = f
	m.gut.file = f
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.hunkCur = -1
	m.pruneSyntax()
	m.resetCursor()
	m.blank = m.blank[:0] // a new document needs an exact line count
	m.cancelAnim()
	m.setContent()
	m.vp.GotoTop()
}

// lineForRow returns the diff line painted at row i, if any. It is what the
// gutter shows, and what the tests compare layouts with.
func (m Model) lineForRow(i int) (diff.Line, bool) {
	if m.doc != nil {
		return m.doc.lineForRow(i)
	}
	if m.file == nil {
		return diff.Line{}, false
	}
	idx := i - m.gut.header - len(m.noteRows)
	if idx < 0 {
		return diff.Line{}, false
	}
	return m.file.LineAt(idx)
}

// textAt returns the searchable text of document line i.
func (m Model) textAt(i int) string {
	if m.doc != nil {
		return m.doc.textAt(i)
	}
	if i < 0 || i >= len(m.plain) {
		return ""
	}
	return m.plain[i]
}

// SetLines lays a single file's diff out as a virtual document: fixed-size
// line blocks, only the ones in view carry rows.
func (m *Model) SetLines(f *diff.File) {
	m.doc = newLineDoc(f, m.preRows(f), m.preText(f), docBlockLines)
	m.gut.lineFor = m.doc.lineForRow
	m.gut.file = nil
	m.file = f
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.hunkCur = -1
	m.pruneSyntax()
	m.cancelAnim()
	m.rows = m.rows[:0]
	m.plain = m.plain[:0]
	m.rendered = nil
	m.doc.setMode(m.split)
	m.rebuildNav()
	m.rebuildDoc()
	m.vp.GotoTop()
}

// preRows are the rows above the diff: the notes, then the file header.
func (m Model) preRows(f *diff.File) []contentRow {
	rows := make([]contentRow, 0, len(m.noteRows)+1)
	for _, n := range m.noteRows {
		rows = append(rows, contentRow{pre: true, text: n})
	}
	// An aggregate document has no path: its totals already live in the notes,
	// so a header row would only repeat them as a bare "+N -M" line.
	if f != nil && f.Path() != "" {
		rows = append(rows, contentRow{pre: true, text: m.renderHeader(f)})
	}
	return rows
}

// preText is the searchable text of those rows, matching what a flat document
// would have put in plain.
func (m Model) preText(f *diff.File) []string {
	out := make([]string, 0, len(m.notePlain)+1)
	out = append(out, m.notePlain...)
	if f != nil && f.Path() != "" {
		out = append(out, f.Path())
	}
	return out
}

// SetNotes replaces the metadata lines rendered above the diff.
func (m *Model) SetNotes(lines []string) {
	m.notes = lines
	m.refreshNoteRows()
	m.gut.notes = len(m.noteRows)
	m.setContent()
}

// File returns the file currently rendered, or nil.
func (m Model) File() *diff.File { return m.file }

// CurrentFilePatch is the patch of the file in view: the whole file in single
// mode, the block under the viewport in a virtual document.
func (m Model) CurrentFilePatch() string {
	if m.doc != nil {
		return m.doc.blockPatchAt(m.vp.YOffset())
	}
	if m.file == nil {
		return ""
	}
	return m.file.Patch()
}

// Empty reports whether there is nothing to display.
func (m Model) Empty() bool {
	if m.doc != nil {
		return m.doc.count() == 0
	}
	return m.file == nil || len(m.rows) == 0
}

// nrows is the number of rendered rows in the document.
func (m Model) nrows() int {
	if m.doc != nil {
		return m.doc.count()
	}
	return len(m.rows)
}

// rowAt returns row i of the document.
func (m Model) rowAt(i int) contentRow {
	if m.doc != nil {
		return m.doc.row(i)
	}
	return m.rows[i]
}

// nlines is the number of document lines, which is what search indexes.
func (m Model) nlines() int {
	if m.doc != nil {
		return m.doc.nlines()
	}
	return len(m.plain)
}

// lineAt returns the searchable text of document line i.
func (m Model) lineAt(i int) string {
	if m.doc != nil {
		return m.doc.textAt(i)
	}
	return m.plain[i]
}

// SetDocument replaces the view with a virtual multi-file document. load
// materialises block i's diff on demand: only a window of blocks keeps its rows.
func (m *Model) SetDocument(load func(i int) *diff.File) {
	m.doc = newBlockDoc(load)
	m.gut.file = nil
	m.gut.lineFor = m.doc.lineForRow
	m.file = nil
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.hunkCur = -1
	m.pruneSyntax()
	m.resetCursor()
	m.cancelAnim()
	m.blank = m.blank[:0] // a new document needs an exact line count
	m.rows = m.rows[:0]
	m.plain = m.plain[:0]
	m.rendered = nil
	m.hunks = m.hunks[:0]
	m.fileBands = m.fileBands[:0]
	m.hunkBlock = m.hunkBlock[:0]
	m.hunkLocal = m.hunkLocal[:0]
	m.blockStaged = m.blockStaged[:0]
	m.doc.setMode(m.split)
	m.rebuildDoc()
	m.vp.GotoTop()
}

// AppendBlocks adds the next files to the virtual document. Their layout is
// recorded immediately, their rows only while they stay in the view window.
func (m *Model) AppendBlocks(files []*diff.File) { m.appendBlocks(files, nil) }

// appendBlocks is AppendBlocks with the band label of each block.
func (m *Model) appendBlocks(files []*diff.File, tags []string) {
	if m.doc == nil || len(files) == 0 {
		return
	}
	m.refreshNoteRows()
	pre := len(m.noteRows)
	if m.hasFileHeader() {
		pre++
	}
	before, preCount := m.doc.nblocks(), m.doc.preCount()
	m.doc.add(files, pre, tags)
	m.doc.setMode(m.split)
	if before == 0 || preCount != pre {
		// The first blocks, or the rows above them moved: rebuild the whole nav.
		m.rebuildNav()
	} else {
		// Streaming a chunk must not re-walk the blocks before it.
		m.appendNav(before)
	}
	m.rebuildDoc()
}

// appendNav records the hunk and band entries for the blocks added since from.
func (m *Model) appendNav(from int) {
	n := m.doc.nblocks()
	for i := max(0, from); i < n; i++ {
		start, hunks := m.doc.layout(i)
		for j, rel := range hunks {
			m.hunks = append(m.hunks, start+rel)
			m.hunkBlock = append(m.hunkBlock, int32(i))
			m.hunkLocal = append(m.hunkLocal, int32(j))
		}
		// A band is a file, not a block: a single-file document is laid out
		// in many blocks, and every one of them would otherwise act on the
		// whole file from a row nobody would call a file header.
		if i == 0 || !m.doc.single() {
			m.fileBands = append(m.fileBands, start)
		}
	}
}

// rebuildNav recomputes the hunk and band lists from the document layout.
func (m *Model) rebuildNav() {
	m.hunks = m.hunks[:0]
	m.fileBands = m.fileBands[:0]
	m.hunkBlock = m.hunkBlock[:0]
	m.hunkLocal = m.hunkLocal[:0]

	n := m.doc.nblocks()
	for i := 0; i < n; i++ {
		start, hunks := m.doc.layout(i)
		for j, rel := range hunks {
			m.hunks = append(m.hunks, start+rel)
			m.hunkBlock = append(m.hunkBlock, int32(i))
			m.hunkLocal = append(m.hunkLocal, int32(j))
		}
		if i == 0 || !m.doc.single() {
			m.fileBands = append(m.fileBands, start)
		}
	}
}

// rebuildDoc refreshes everything derived from the layout: nav, width, rows.
func (m *Model) rebuildDoc() {
	m.cache.reset()
	m.updateGutterWidth()
	m.doc.setGutter(m.gut.cells(), m.gut.numCells()+1)
	m.contentWidth = m.doc.width(m.vpWidth(), m.split, m.showLineNumbers)
	m.pushContent()
}

// BlockUpdate says which block of a document a fresh diff belongs to.
type BlockUpdate struct {
	// Index is the block, in document order, which is the listing's row order.
	Index int
	File  *diff.File
}

// ReplaceBlocks swaps fresh diffs into blocks the document already holds and lays
// it out again around them, so a live change costs one file. Blocks keep their
// position, so bands, hunks and the cursor's block stay what they were.
func (m *Model) ReplaceBlocks(updates []BlockUpdate) bool {
	if m.doc == nil || len(updates) == 0 {
		return false
	}
	files := make(map[int]*diff.File, len(updates))
	for _, u := range updates {
		files[u.Index] = u.File
	}
	olds := make([]*diff.File, 0, len(updates))
	for _, u := range updates {
		olds = append(olds, m.doc.fileAt(u.Index))
	}
	if !m.doc.replace(files) {
		return false
	}
	m.forgetSyntax(olds)
	// A block's row count can change with its content: recount exactly.
	m.blank = m.blank[:0]
	m.rebuildNav()
	m.rebuildDoc()
	return true
}

// forgetSyntax drops the token indexes of diffs the document no longer shows.
func (m *Model) forgetSyntax(files []*diff.File) {
	if len(m.syn) == 0 {
		return
	}
	for _, f := range files {
		if f == nil {
			continue
		}
		for src := range m.syn {
			if src.file == f {
				delete(m.syn, src)
			}
		}
	}
}

// DocCounts is the changed lines the document holds, which the header reports.
func (m Model) DocCounts() (adds, dels int) {
	if m.doc == nil {
		return 0, 0
	}
	return m.doc.counts()
}

// DocBlocks reports how many blocks a document holds and how many carry rows.
func (m Model) DocBlocks() (blocks, materialised int) {
	if m.doc == nil {
		return 0, 0
	}
	return m.doc.nblocks(), m.doc.materialised()
}

// Update processes animation frames and forwards everything else to the
// viewport. With smooth scrolling on, a discrete step moves the animation target
// instead of the viewport, so every way of moving glides and presses accumulate.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if frame, ok := msg.(scrollFrameMsg); ok {
		if frame.seq != m.animSeq {
			return m, nil
		}
		return m, m.updateScroll(frame.at)
	}

	scroll := isScrollInput(msg)
	before := m.vp.YOffset()
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	after := m.vp.YOffset()

	// Only a message that moved the window may touch the cursor: a stray one arriving
	// during a glide would drag the cursor and cancel the animation halfway.
	if !scroll || after == before {
		return m, cmd
	}

	if !m.smooth {
		m.keepCursorInView()
		return m, cmd
	}

	step := float64(after - before)
	if m.anim {
		// Retarget the running animation; the step is undone on the viewport so the frame
		// does not jump ahead of the spring, and the cursor is left alone.
		m.target = m.clampTarget(m.target + step)
		m.vp.SetYOffset(before)
		return m, cmd
	}
	if math.Abs(step) <= smallStepRows {
		// A line or a wheel tick lands now; only the cursor has to catch up.
		m.keepCursorInView()
		return m, cmd
	}

	// Start the animation from the offset the step began at.
	m.vp.SetYOffset(before)
	return m, tea.Batch(cmd, m.ScrollTo(max(0, min(after, m.maxScroll()))))
}

// clampTarget keeps an animation target inside the scrollable range.
func (m Model) clampTarget(target float64) float64 {
	return math.Max(0, math.Min(target, float64(m.maxScroll())))
}

// maxScroll is the highest viewport offset that still shows content.
func (m Model) maxScroll() int {
	return max(0, m.nrows()-m.vpHeight())
}

// View renders the visible window with its scrollbars. Painting is O(viewport),
// not O(file): only the rows in view are styled.
func (m Model) View() string {
	if !m.bars() {
		if m.barWidth() == 0 {
			return m.vp.View()
		}
		return m.wrapWithBar()
	}

	w, h := m.vpWidth(), m.vpHeight()
	lines := m.visibleLines(w, h)

	thumbStart, thumbLen := m.vbar(len(lines))
	for i := range lines {
		if pad := w - lipgloss.Width(lines[i]); pad > 0 {
			lines[i] += strings.Repeat(" ", pad)
		}
		if i >= thumbStart && i < thumbStart+thumbLen {
			lines[i] += m.styles.ScrollThumb("┃")
		} else {
			lines[i] += m.styles.ScrollTrack("│")
		}
	}

	if m.hscroll() {
		// The bar closes the diff. When the search line is showing the app
		// sizes this pane one row shorter, so the bar lands right above it.
		lines = append(lines, m.hbar())
	}
	return strings.Join(lines, "\n")
}

// vbar computes the vertical scrollbar thumb position and size.
func (m Model) vbar(visible int) (start, length int) {
	total := m.nrows()
	if total <= visible {
		return 0, 0
	}
	length = max(1, visible*visible/total)
	maxOff := total - visible
	pos := min(max(0, m.vp.YOffset()), maxOff)
	start = pos * (visible - length) / maxOff
	return start, length
}

// hbar renders the horizontal scrollbar row.
func (m Model) hbar() string {
	w := m.vpWidth()
	if w <= 0 {
		return ""
	}
	if m.contentWidth <= w {
		return strings.Repeat(" ", w+scrollbarSize)
	}

	thumbLen := max(1, w*w/m.contentWidth)
	maxOff := m.contentWidth - w
	pos := min(max(0, m.vp.XOffset()), maxOff)
	start := pos * (w - thumbLen) / maxOff

	var b strings.Builder
	for i := 0; i < w; i++ {
		if i >= start && i < start+thumbLen {
			b.WriteString(m.styles.ScrollThumb("━"))
		} else {
			b.WriteString(m.styles.ScrollTrack("─"))
		}
	}
	b.WriteString(strings.Repeat(" ", scrollbarSize))
	return b.String()
}

// XOffset returns the horizontal scroll position.
func (m Model) XOffset() int { return m.vp.XOffset() }

// YOffset returns the vertical scroll position, in rendered rows.
func (m Model) YOffset() int { return m.vp.YOffset() }

// ScrollPercent returns the vertical scroll position, 0..1.
func (m Model) ScrollPercent() float64 { return m.vp.ScrollPercent() }

// PastBottom reports whether the viewport has scrolled past the bottom.
func (m Model) PastBottom() bool { return m.vp.PastBottom() }

// MatchCount returns the number of search matches for the current term.
func (m Model) MatchCount() int { return len(m.matches) }

// SearchTerm returns the active search term, if any.
func (m Model) SearchTerm() string { return m.term }

// SearchResultsMsg carries the complete match list of a background search. Seq
// identifies the search it belongs to, so stale results are dropped.
type SearchResultsMsg struct {
	Seq     int
	Term    string
	Matches []Match
}

// Search scans the rows in view first, so highlights appear at once, then the
// whole document in the background, delivered as SearchResultsMsg.
func (m *Model) Search(term string) (int, tea.Cmd) {
	m.term = term
	m.rx = searchRegexp(term)
	m.searchSeq++
	m.matches = nil
	m.matchIdx = -1
	if term == "" {
		m.rebuild()
		return 0, nil
	}

	// Phase one: what is on screen right now.
	m.ensureFolded()
	from, to := m.visibleLineRange()
	m.matches = m.scanWindow(term, from, to)
	m.rebuild()

	// Phase two: everything, off the UI goroutine.
	return len(m.matches), m.scanAllCmd(term)
}

// scanWindow finds the matches of term in document lines [from, to).
func (m Model) scanWindow(term string, from, to int) []Match {
	if m.rx != nil {
		return scanRegex(m.nlines(), m.lineAt, m.rx, from, to, m.lineBase())
	}
	if got, ok := m.sourceMatches(term, from, to); ok {
		return got
	}
	return scanLines(m.nlines(), m.lineAt, m.foldedFor(), term, from, to, m.lineBase())
}

// scanAllCmd scans the whole document off the UI goroutine. A single paged file
// scans its sources; everything else folds line by line.
func (m Model) scanAllCmd(term string) tea.Cmd {
	if m.nlines() == 0 {
		return nil
	}
	seq := m.searchSeq
	if m.rx != nil {
		rx := m.rx
		count, text, base := m.nlines(), m.lineAt, m.lineBase()
		return func() tea.Msg {
			return SearchResultsMsg{Seq: seq, Term: term, Matches: scanRegex(count, text, rx, 0, count, base)}
		}
	}
	if m.doc != nil && m.doc.src != nil && m.doc.src.Paged() {
		src, pre, preTxt, base := m.doc.src, len(m.doc.pre), m.doc.preTxt, m.lineBase()
		count, text, folded := m.nlines(), m.lineAt, m.foldedFor()
		return func() tea.Msg {
			matches, ok := sourceMatches(src, pre, preTxt, base, term, 0, pre+src.NLines())
			if !ok {
				// The term needs the rendered text (a sign, or non-ASCII):
				// scan the lines instead.
				matches = scanLines(count, text, folded, term, 0, count, base)
			}
			return SearchResultsMsg{Seq: seq, Term: term, Matches: matches}
		}
	}
	return scanLinesCmd(m.nlines(), m.lineAt, m.foldedFor(), term, seq, m.lineBase())
}

// sourceMatches finds term in document lines [from, to) by searching a paged file's
// own sources, which keeps a search over millions of rows cheap.
func (m Model) sourceMatches(term string, from, to int) ([]Match, bool) {
	if m.doc == nil || m.doc.src == nil || !m.doc.src.Paged() {
		return nil, false
	}
	return sourceMatches(m.doc.src, len(m.doc.pre), m.doc.preTxt, m.lineBase(), term, from, to)
}

// sourceMatches is the scan behind sourceMatches, split out so it never touches the
// model.
func sourceMatches(src *diff.File, pre int, preTxt []string, base int, term string, from, to int) ([]Match, bool) {
	var out []Match
	lo, hi := max(0, from), min(to, pre)
	for line := lo; line < hi; line++ {
		for _, r := range findAllFold(preTxt[line], term) {
			out = append(out, Match{Line: base + line, Start: r[0], End: r[1]})
		}
	}

	rowFrom, rowTo := from-pre, to-pre
	if rowTo > 0 {
		rows, ok := src.MatchRows(term, rowFrom, rowTo)
		if !ok {
			return nil, false
		}
		for _, r := range rows {
			out = append(out, Match{Line: base + pre + r.Row, Start: r.Start, End: r.End})
		}
	}
	return out, true
}

// lineBase converts a document line index into the flat index the rows use: the two
// coincide for a flat document, a virtual one starts after the notes and header.
func (m Model) lineBase() int {
	if m.doc != nil {
		return m.doc.flatBase()
	}
	return 0
}

// visibleLineRange is the span of lines the rows in view cover.
func (m Model) visibleLineRange() (int, int) {
	if m.doc != nil {
		return m.doc.lineRange(m.vp.YOffset(), m.vpHeight())
	}
	from := max(0, m.vp.YOffset())
	return from, min(m.nlines(), from+m.vpHeight())
}

// foldedFor returns the lowercase mirror of a flat document, if built.
func (m Model) foldedFor() []string {
	if m.doc != nil {
		return nil
	}
	return m.folded
}

// ApplySearchResults installs the completed match list and jumps to the first
// match at or below the current position.
func (m *Model) ApplySearchResults(msg SearchResultsMsg) bool {
	if msg.Seq != m.searchSeq || msg.Term != m.term || m.term == "" {
		return false
	}
	m.matches = msg.Matches
	m.matchIdx = -1
	if len(m.matches) > 0 {
		m.matchIdx = m.nearestMatch(m.vp.YOffset())
	}
	m.rebuild()
	if m.matchIdx >= 0 {
		m.showMatch()
	}
	return true
}

// ensureFolded builds the ASCII-lowercase mirror the fast scanner uses.
func (m *Model) ensureFolded() {
	if m.doc != nil {
		m.folded = nil
		return
	}
	if len(m.folded) == len(m.plain) {
		return
	}
	m.folded = make([]string, len(m.plain))
	for i, line := range m.plain {
		m.folded[i] = foldASCII(line)
	}
}

// scanLines collects the matches of term in lines[from:to] in row coordinates;
// folded is an optional lowercase mirror of the whole document.
func scanLines(count int, text func(int) string, folded []string, term string, from, to, lineBase int) []Match {
	from = max(0, from)
	to = min(to, count)
	if from >= to || term == "" {
		return nil
	}

	if isASCII(term) {
		lower := foldASCII(term)
		if lower == "" {
			return nil
		}
		var out []Match

		if folded != nil {
			// Flat document: the mirror is built once, so every line is a plain search.
			for i := from; i < to && i < len(folded); i++ {
				line := folded[i]
				for at := 0; at <= len(line)-len(lower); {
					j := strings.Index(line[at:], lower)
					if j < 0 {
						break
					}
					start := at + j
					out = append(out, Match{Line: lineBase + i, Start: start, End: start + len(lower)})
					at = start + len(lower)
				}
			}
			return out
		}

		// Virtual document: fold into a scratch buffer, which keeps the scan
		// allocation-free.
		lowerB := []byte(lower)
		var scratch []byte
		for i := from; i < to; i++ {
			scratch = foldInto(scratch[:0], text(i))
			for at := 0; at <= len(scratch)-len(lowerB); {
				j := bytes.Index(scratch[at:], lowerB)
				if j < 0 {
					break
				}
				start := at + j
				out = append(out, Match{Line: lineBase + i, Start: start, End: start + len(lowerB)})
				at = start + len(lowerB)
			}
		}
		return out
	}

	var out []Match
	for i := from; i < to; i++ {
		for _, r := range findAllFold(text(i), term) {
			out = append(out, Match{Line: lineBase + i, Start: r[0], End: r[1]})
		}
	}
	return out
}

// searchRegexp compiles term as a case-insensitive pattern when it reads as one.
// A plain word stays a substring search; a bad pattern falls back to literal.
func searchRegexp(term string) *regexp.Regexp {
	if !strings.ContainsAny(term, `\.+*?()[]{}^$|`) {
		return nil
	}
	rx, err := regexp.Compile("(?i)" + term)
	if err != nil {
		return nil
	}
	return rx
}

// scanRegex finds every match of rx in document lines [from, to).
func scanRegex(count int, text func(int) string, rx *regexp.Regexp, from, to, lineBase int) []Match {
	from = max(0, from)
	to = min(to, count)
	var out []Match
	for i := from; i < to; i++ {
		for _, loc := range rx.FindAllStringIndex(text(i), -1) {
			if loc[1] > loc[0] {
				out = append(out, Match{Line: lineBase + i, Start: loc[0], End: loc[1]})
			}
		}
	}
	return out
}

// foldInto appends the ASCII-lowercased form of s to dst.
func foldInto(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		dst = append(dst, c)
	}
	return dst
}

// scanLinesCmd scans the whole document off the UI goroutine.
func scanLinesCmd(count int, text func(int) string, folded []string, term string, seq, lineBase int) tea.Cmd {
	if count == 0 {
		return nil
	}
	return func() tea.Msg {
		return SearchResultsMsg{
			Seq:     seq,
			Term:    term,
			Matches: scanLines(count, text, folded, term, 0, count, lineBase),
		}
	}
}

// indexAll recomputes the match list without scrolling.
func (m *Model) indexAll() {
	m.matches = nil
	if m.term == "" {
		m.matchIdx = -1
		return
	}
	m.matches = scanLines(m.nlines(), m.lineAt, m.foldedFor(), m.term, 0, m.nlines(), m.lineBase())
	if m.matchIdx >= len(m.matches) {
		m.matchIdx = len(m.matches) - 1
	}
}

// reindex refreshes the search state after the plain text grew.
func (m *Model) reindex() {
	if m.term == "" {
		return
	}
	m.indexAll()
}

// foldASCII lowers ASCII letters without changing byte offsets.
func foldASCII(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// isASCII reports whether every byte is plain ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ClearSearch removes the search highlights.
func (m *Model) ClearSearch() {
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.rebuild()
}

// HighlightNext moves to the next search match.
func (m *Model) HighlightNext() {
	if len(m.matches) == 0 {
		return
	}
	m.matchIdx = (m.matchIdx + 1) % len(m.matches)
	m.rebuild()
	m.showMatch()
}

// HighlightPrevious moves to the previous search match.
func (m *Model) HighlightPrevious() {
	if len(m.matches) == 0 {
		return
	}
	m.matchIdx = (m.matchIdx - 1 + len(m.matches)) % len(m.matches)
	m.rebuild()
	m.showMatch()
}

func (m *Model) showMatch() {
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) {
		return
	}
	mt := m.matches[m.matchIdx]
	before := m.vp.YOffset()
	m.vp.EnsureVisible(m.rowForLine(mt.Line), mt.Start, mt.End)
	if after := m.vp.YOffset(); after != before {
		// EnsureVisible scrolls sideways, which is wanted; the vertical part glides.
		m.vp.SetYOffset(before)
		m.glideTo(after)
	}
}

// nearestMatch is the first match at or below row from.
func (m Model) nearestMatch(from int) int {
	for i, mt := range m.matches {
		if m.rowForLine(mt.Line) >= from {
			return i
		}
	}
	return len(m.matches) - 1
}

// rowForLine maps a flat line index onto the row that paints it.
func (m Model) rowForLine(flat int) int {
	if m.doc != nil {
		return m.doc.rowForLine(flat - m.lineBase())
	}
	return flat
}

// ScrollTo glides (or jumps) to a rendered-line offset and returns the command
// that drives the animation.
func (m *Model) ScrollTo(line int) tea.Cmd {
	line = max(0, min(line, m.maxScroll()))
	if !m.smooth {
		m.vp.SetYOffset(line)
		return nil
	}
	m.pos = float64(m.vp.YOffset())
	m.vel = 0
	m.target = float64(line)
	if math.Abs(m.target-m.pos) <= smallStepRows {
		// Too short to be worth animating: land on it right away.
		m.cancelAnim()
		m.vp.SetYOffset(line)
		return nil
	}
	m.anim = true
	m.lastFrame = time.Time{}
	m.animSeq++
	return tickScroll(m.animSeq)
}

func (m *Model) JumpTo(line int) {
	m.cancelAnim()
	m.ensureContent()
	m.vp.SetYOffset(max(0, line))
}

// SetScroll puts the top of the viewport at a row, clamped to the document, which
// is how a reload puts the reader back.
func (m *Model) SetScroll(row int) {
	m.cancelAnim()
	m.vp.SetYOffset(max(0, min(row, max(0, m.nrows()-1))))
}

// Anchor is where the reader is: the flat line at the top of the viewport, which a
// refresh replays with SetFileAt. A flat document has no line index, so rows serve.
func (m Model) Anchor() int {
	if m.doc != nil {
		return m.doc.lineAtRow(m.vp.YOffset()) + m.lineBase()
	}
	return m.vp.YOffset()
}

// TopLine is the diff line painted at the top of the pane, the reader's place in the
// content rather than in the rows: staging removes rows above them.
func (m Model) TopLine() int {
	if m.doc != nil {
		return m.Anchor()
	}
	idx := m.vp.YOffset() - m.gut.header - len(m.noteRows)
	if m.file == nil {
		return max(0, idx)
	}
	return max(0, min(idx, m.file.NLines()-1))
}

// rowForDiffLine is the row that paints a diff line.
func (m Model) rowForDiffLine(line int) int {
	if m.doc != nil {
		return m.doc.rowForLine(line - m.lineBase())
	}
	return line + m.gut.header + len(m.noteRows)
}

// SetFileAt shows f with the viewport on flat line, without animating.
func (m *Model) SetFileAt(f *diff.File, line int) {
	m.SetFile(f)
	m.JumpToLine(line)
}

// JumpToLine puts the viewport on the row that paints flat line, immediately.
func (m *Model) JumpToLine(line int) {
	m.JumpTo(m.rowForLine(max(0, line)))
}

// CancelScroll stops any running scroll animation.
func (m *Model) CancelScroll() { m.cancelAnim() }

// glideTo moves the window to a row, gliding when smooth scrolling is on. The frame
// command is kept for TakeScrollCmd, since a cursor movement cannot return it.
func (m *Model) glideTo(row int) { m.scrollCmd = m.ScrollTo(row) }

// TakeScrollCmd hands over the command driving the last glide, and forgets it.
func (m *Model) TakeScrollCmd() tea.Cmd {
	c := m.scrollCmd
	m.scrollCmd = nil
	return c
}

func (m *Model) cancelAnim() {
	m.anim = false
	m.vel = 0
	m.lastFrame = time.Time{}
	m.animSeq++
	m.scrollCmd = nil
}

func (m *Model) updateScroll(now time.Time) tea.Cmd {
	if !m.anim {
		return nil
	}

	// Advance by the time that actually elapsed, capped so a long gap does not fling
	// the view.
	dt := now.Sub(m.lastFrame)
	if m.lastFrame.IsZero() || dt <= 0 {
		dt = frameInterval
	}
	dt = max(frameInterval/4, min(dt, 50*time.Millisecond))
	m.lastFrame = now

	m.target = max(0, min(m.target, float64(m.maxScroll())))
	// Stiffness grows with the distance left; rebuilt per frame because the spring
	// bakes its timestep into its coefficients.
	freq := baseSpringFreq + math.Min(math.Abs(m.target-m.pos)*0.6, maxSpringFreq-baseSpringFreq)
	m.spring = harmonica.NewSpring(dt.Seconds(), freq, 1.0)
	m.pos, m.vel = m.spring.Update(m.pos, m.vel, m.target)

	if math.Abs(m.target-m.pos) < 0.02 && math.Abs(m.vel) < 0.5 {
		m.pos, m.vel, m.anim = m.target, 0, false
		m.vp.SetYOffset(int(math.Round(m.target)))
		// A wheel scroll names no row, so once it lands the cursor is brought back on
		// screen. During the glide it is left alone: dragging it would cancel it.
		m.keepCursorInView()
		// Bringing the cursor back is itself a move of the window, and it glides.
		return m.TakeScrollCmd()
	}
	m.vp.SetYOffset(int(math.Round(m.pos)))
	return tickScroll(m.animSeq)
}

func tickScroll(seq int) tea.Cmd {
	return tea.Tick(frameInterval, func(at time.Time) tea.Msg {
		return scrollFrameMsg{seq: seq, at: at}
	})
}

// NextHunk moves to the next hunk header. The first press from above the first
// hunk lands on that hunk.
func (m *Model) NextHunk() tea.Cmd {
	next := m.CurrentHunkIndex() + 1
	if next < 0 || next >= len(m.hunks) {
		return nil
	}
	m.hunkCur = next
	return m.goToRow(m.hunks[next])
}

// PrevHunk moves to the previous hunk header, if any.
func (m *Model) PrevHunk() tea.Cmd {
	cur := m.CurrentHunkIndex()
	if cur > 0 {
		m.hunkCur = cur - 1
		return m.goToRow(m.hunks[cur-1])
	}
	m.hunkCur = 0
	return m.goToRow(0)
}

// goToRow is how a navigation key moves the pane: with a staging cursor the cursor
// goes to the row, without one only the window moves.
func (m *Model) goToRow(row int) tea.Cmd {
	if !m.Selectable() {
		return m.ScrollTo(row)
	}
	m.setCursor(row)
	// A jump reads best with its row at the top of the pane.
	m.glideTo(row)
	return nil
}

// PageDown moves the reader a screenful on, and PageUp a screenful back.
func (m *Model) PageDown() tea.Cmd {
	if !m.Selectable() {
		return m.ScrollTo(min(m.vp.YOffset()+m.vpHeight(), m.maxScroll()))
	}
	m.moveByPage(m.vpHeight())
	return nil
}

// PageUp moves the reader a screenful back.
func (m *Model) PageUp() tea.Cmd {
	if !m.Selectable() {
		return m.ScrollTo(max(0, m.vp.YOffset()-m.vpHeight()))
	}
	m.moveByPage(-m.vpHeight())
	return nil
}

// GotoTop moves to the first row a keystroke can act on.
func (m *Model) GotoTop() tea.Cmd {
	if !m.Selectable() {
		return m.ScrollTo(0)
	}
	// A document materialises only a window of blocks: bring this row's back.
	m.rowAt(0)
	if row := m.firstItemRow(0); row >= 0 {
		m.setCursor(row)
	}
	m.glideTo(0)
	return nil
}

// GotoBottom moves to the last row a keystroke can act on; the trailing context
// is not something to act on.
func (m *Model) GotoBottom() tea.Cmd {
	if !m.Selectable() {
		m.ensureContent()
		return m.scrollToEnd()
	}
	m.ensureContent()
	m.rowAt(max(0, m.nrows()-1))
	if row := m.lastItemRow(); row >= 0 {
		m.setCursor(row)
	}
	// The last thing to act on is rarely the last row, so the window goes to the end.
	m.glideTo(m.maxScroll())
	return nil
}

// scrollToEnd puts the window at the bottom without touching the cursor.
func (m *Model) scrollToEnd() tea.Cmd {
	if !m.smooth {
		m.vp.GotoBottom()
		return nil
	}
	m.pos = float64(m.vp.YOffset())
	m.vel = 0
	m.target = float64(max(0, m.nrows()))
	m.anim = true
	m.animSeq++
	return tickScroll(m.animSeq)
}

// JumpToBand scrolls smoothly to the per-file band with the given index.
func (m *Model) JumpToBand(i int) tea.Cmd {
	if i < 0 || i >= len(m.fileBands) {
		return nil
	}
	return m.ScrollTo(m.fileBands[i])
}

// JumpToBandNow jumps to a band immediately, so rapid selection changes never
// queue animations.
func (m *Model) JumpToBandNow(i int) {
	if i < 0 || i >= len(m.fileBands) {
		return
	}
	m.hunkCur = -1
	m.JumpTo(m.fileBands[i])
}

// GotoBand moves the cursor into block i and glides the window to it, so the
// sidebar and the pane agree about which file a keystroke acts on.
func (m *Model) GotoBand(i int) {
	if !m.HasBand(i) {
		return
	}
	if m.Selectable() {
		m.CursorToBand(i)
	}
	m.glideTo(max(0, m.fileBands[i]))
}

// HasBand reports whether band i has been laid out yet.
func (m Model) HasBand(i int) bool { return i >= 0 && i < len(m.fileBands) }

// CurrentHunkIndex returns the hunk a keystroke acts on, or -1: the one the cursor
// is in, or without a cursor the one at the top of the window.
func (m Model) CurrentHunkIndex() int {
	cur := m.vp.YOffset()
	if m.HasCursor() {
		cur = m.cursor
	}
	if m.hunkCur >= 0 && m.hunkCur < len(m.hunks) {
		h := m.hunks[m.hunkCur]
		if h >= m.vp.YOffset() && h < m.vp.YOffset()+m.vpHeight() {
			return m.hunkCur
		}
	}
	idx := -1
	for i, h := range m.hunks {
		if h <= cur {
			idx = i
		}
	}
	return idx
}

// CurrentHunkPatch returns the raw patch text of the hunk in view.
func (m Model) CurrentHunkPatch() string {
	idx := m.CurrentHunkIndex()
	if idx < 0 {
		return ""
	}
	if m.doc != nil {
		block, local := -1, -1
		if idx < len(m.hunkBlock) {
			block, local = int(m.hunkBlock[idx]), int(m.hunkLocal[idx])
		}
		return m.doc.patchAt(idx, block, local)
	}
	if m.file == nil || idx >= m.file.NHunks() {
		return ""
	}
	return m.file.HunkPatch(idx)
}

// buildRows lays out the content rows and their searchable text for this mode.
func (m *Model) buildRows() {
	m.rows = m.rows[:0]
	m.plain = m.plain[:0]
	m.folded = nil
	m.indexed = 0
	m.laid = 0

	for _, n := range m.noteRows {
		m.rows = append(m.rows, contentRow{pre: true, text: n})
	}
	m.plain = append(m.plain, m.notePlain...)

	if m.file != nil && m.file.Path() != "" {
		m.rows = append(m.rows, contentRow{pre: true, text: m.renderHeader(m.file)})
		m.plain = append(m.plain, m.file.Path())
	}
	if m.file != nil {
		m.appendRows(0)
	}
}

// appendRows lays out lines from index from onwards, keeping the rows before it.
func (m *Model) appendRows(from int) {
	if m.file == nil || from >= m.file.NLines() {
		return
	}
	base := len(m.notes) + 1
	lines := m.file.LinesRange(from, m.file.NLines())

	if m.split {
		m.rows = append(m.rows, splitRows(lines, base+from)...)
	} else {
		for i, l := range lines {
			m.rows = append(m.rows, contentRow{
				fullWidth: true,
				line:      l,
				flat:      base + from + i,
				hunk:      l.Kind == diff.HunkHeader,
			})
		}
	}
	for _, l := range lines {
		m.plain = append(m.plain, l.Text)
	}
	m.laid = from + len(lines)
}

// splitRows pairs deletion and addition runs into side-by-side rows.
func splitRows(lines []diff.Line, base int) []contentRow {
	rows := make([]contentRow, 0, len(lines))

	for i := 0; i < len(lines); {
		l := lines[i]
		if l.Kind == diff.Deletion || l.Kind == diff.Addition {
			start := i
			for i < len(lines) && (lines[i].Kind == diff.Deletion || lines[i].Kind == diff.Addition) {
				i++
			}
			var dels, adds []int
			for j := start; j < i; j++ {
				switch lines[j].Kind {
				case diff.Deletion:
					dels = append(dels, j)
				case diff.Addition:
					adds = append(adds, j)
				}
			}
			for k := 0; k < max(len(dels), len(adds)); k++ {
				var r contentRow
				if k < len(dels) {
					r.left, r.hasLeft, r.leftFlat = lines[dels[k]], true, base+dels[k]
				}
				if k < len(adds) {
					r.right, r.hasRight, r.rightFlat = lines[adds[k]], true, base+adds[k]
				}
				rows = append(rows, r)
			}
			continue
		}

		full := l.Kind == diff.HunkHeader || l.Kind == diff.NoNewline
		rows = append(rows, contentRow{
			fullWidth: full,
			line:      l,
			flat:      base + i,
			hunk:      l.Kind == diff.HunkHeader,
			left:      l,
			hasLeft:   true,
			leftFlat:  base + i,
			right:     l,
			hasRight:  true,
			rightFlat: base + i,
		})
		i++
	}

	return rows
}

// setContent lays out the rows for this mode and refreshes the viewport index.
func (m *Model) setContent() {
	m.refreshNoteRows()
	if m.doc != nil {
		m.rebuild()
		return
	}
	m.buildRows()
	m.rebuild()
}

// rebuild refreshes the search index, navigation stops, content width and the line
// count. Wrap mode renders every row, since wrapped height depends on the text.
func (m *Model) rebuild() {
	m.refreshNoteRows()
	m.gut.header = 0
	if m.hasFileHeader() {
		m.gut.header = 1
	}
	m.updateGutterWidth()
	if m.doc != nil {
		m.doc.setMode(m.split)
		if m.doc.single() {
			m.doc.setPre(m.preRows(m.file), m.preText(m.file))
		}
		m.rebuildNav()
		m.rebuildDoc()
		return
	}

	m.cache.reset()

	m.hunks = m.hunks[:0]
	m.fileBands = m.fileBands[:0]
	m.hunkBlock = m.hunkBlock[:0]
	m.hunkLocal = m.hunkLocal[:0]
	m.contentWidth = 0
	m.indexed = 0
	m.indexRows()
	m.pushContent()
}

// indexRows folds the rows laid out since the last pass into the navigation stops
// and the content width; appending only adds rows at the end.
func (m *Model) indexRows() {
	for i := m.indexed; i < len(m.rows); i++ {
		r := m.rows[i]
		if r.hunk {
			m.hunks = append(m.hunks, i)
			if strings.HasPrefix(r.line.Text, "── ") {
				m.fileBands = append(m.fileBands, i)
			}
		}
		m.contentWidth = max(m.contentWidth, m.rowWidth(r))
	}
	m.indexed = len(m.rows)
}

// contentSyncStep is how far the viewport's line count may lag the document.
const contentSyncStep = 1024

// pushContent hands the row count to the viewport; non-wrapped mode feeds blank
// lines and styles the window on demand.
func (m *Model) pushContent() {
	if m.vp.SoftWrap {
		n := m.nrows()
		rendered := make([]string, n)
		width := max(1, m.vp.Width())
		// A wrapped row is several display lines tall, so each records where it starts.
		if cap(m.wrapPrefix) < n+1 {
			m.wrapPrefix = make([]int, n+1)
		} else {
			m.wrapPrefix = m.wrapPrefix[:n+1]
		}
		lines := 0
		for i := 0; i < n; i++ {
			m.wrapPrefix[i] = lines
			rendered[i] = m.renderRow(i, m.rowAt(i))
			lines += max(1, (ansi.StringWidth(rendered[i])+width-1)/width)
		}
		m.wrapPrefix[n] = lines
		m.rendered = rendered
		m.vp.SetContentLines(rendered)
		for _, l := range rendered {
			m.contentWidth = max(m.contentWidth, ansi.StringWidth(l))
		}
		return
	}

	m.rendered = nil
	// The viewport only needs the line count; the first line carries the content width
	// so horizontal scrolling still clamps, and it is never painted here.
	n := m.nrows()
	// SetContentLines walks every line, so syncing on each chunk is quadratic; a count
	// slightly behind is enough to scroll by, and SyncContent makes it exact.
	if len(m.blank) > 0 && n > len(m.blank) && n-len(m.blank) < contentSyncStep {
		return
	}
	if cap(m.blank) < n {
		m.blank = make([]string, n)
	}
	m.blank = m.blank[:n]
	// Only the first entry ever carries anything: clearing the slice would cost the
	// document per chunk.
	if n > 0 && m.blank[0] != "" {
		m.blank[0] = ""
	}
	if n > 0 && m.contentWidth > m.vpWidth() {
		m.blank[0] = strings.Repeat(" ", m.contentWidth)
	}
	m.vp.SetContentLines(m.blank)
	// The horizontal bar only takes a row when needed, so the height follows both.
	m.vp.SetWidth(m.vpWidth())
	m.vp.SetHeight(m.vpHeight())
}

// ExtendFile appends blocks, keeping the rows already laid out, the scroll position
// and the search, so a document never jumps under the reader.
func (m *Model) ExtendFile(f *diff.File) {
	if m.file == nil || f == nil {
		m.SetFile(f)
		return
	}

	from := m.laid
	m.file = f
	m.gut.file = f
	if from > f.NLines() {
		// Not an append: relayout from scratch.
		m.setContent()
		m.reindex()
		return
	}

	m.appendRows(from)
	m.folded = nil
	m.reindex()
	m.indexRows()
	m.pushContent()
}

// rowWidth is one row's contribution to the content width, which drives the
// horizontal scrollbar.
func (m Model) rowWidth(r contentRow) int {
	if m.split {
		colW := m.vpWidth() / 2
		gut := 0
		if m.showLineNumbers {
			gut = m.gut.numW + 1
		}
		contentW := max(1, colW-gut)
		widest := 0
		if r.hasLeft {
			widest = max(widest, ansi.StringWidth(r.left.Text))
		}
		if r.hasRight {
			widest = max(widest, ansi.StringWidth(r.right.Text))
		}
		if widest <= contentW {
			return m.vpWidth()
		}
		return m.vpWidth() + (widest - contentW)
	}

	if r.pre {
		return ansi.StringWidth(r.text)
	}
	gut := 0
	if m.showLineNumbers {
		gut = m.gut.cells()
	}
	return gut + ansi.StringWidth(r.line.Text)
}

// visibleLines renders the rows currently in view.
func (m Model) visibleLines(w, h int) []string {
	yoff := m.vp.YOffset()
	start := min(yoff, m.nrows())
	end := min(start+h, m.nrows())

	if m.cache.xoff != m.vp.XOffset() || m.cache.width != w {
		m.cache.xoff, m.cache.width = m.vp.XOffset(), w
		m.cache.reset()
	}

	out := make([]string, 0, h)
	for i := start; i < end; i++ {
		line, ok := m.cache.rows[i]
		if !ok {
			line = m.paintRow(i)
			if len(m.cache.rows) < maxCachedRows {
				m.cache.rows[i] = line
			}
		}
		// The cursor bar is prepended outside the cache: it changes with a keystroke.
		out = append(out, m.barAt(i)+line)
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

// paintRow styles, clips and pads one row. The result depends only on the row, the
// width and the horizontal offset, which is what the cache keys on.
func (m Model) paintRow(i int) string {
	gutW := 0
	if m.showLineNumbers && !m.split {
		gutW = m.gut.cells()
	}
	contentW := max(0, m.rowBudget()-gutW)
	x := m.vp.XOffset()

	var text string
	if m.split {
		// Split columns are laid out to the width they are given, so the bar comes out of
		// the budget instead of overflowing.
		text = m.splitRow(i, contentW)
	} else {
		text = m.renderRow(i, m.rowAt(i))
		switch {
		case x > 0:
			text = ansi.Cut(text, x, x+contentW)
		case ansi.StringWidth(text) > contentW:
			text = ansi.Cut(text, 0, contentW)
		}
	}

	// The bar is prepended by visibleLines, so this depends only on document and width.
	line := gutterText(m.gut, viewport.GutterContext{Index: i, TotalLines: m.nrows()}) + text
	if pad := m.rowBudget() - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

// spansFor returns the match run of one line; matches are sorted, so this is a
// binary search instead of a map.
func (m Model) spansFor(line int) []Match {
	i := sort.Search(len(m.matches), func(k int) bool { return m.matches[k].Line >= line })
	if i >= len(m.matches) || m.matches[i].Line != line {
		return nil
	}
	j := i
	for j < len(m.matches) && m.matches[j].Line == line {
		j++
	}
	return m.matches[i:j]
}

func (m Model) renderRow(row int, r contentRow) string {
	switch {
	case r.pre:
		return r.text
	case r.fullWidth:
		return m.renderLine(row, r.line, r.flat)
	default:
		return m.renderSplitRow(row)
	}
}

func (m Model) renderHeader(f *diff.File) string {
	if m.split {
		// Split mode gets a header per column, with the change counts.
		w := m.rowBudget()
		leftW := w / 2
		rightW := w - leftW
		return m.headerSide(pathOr(f.OldPath, f), f, leftW) +
			m.headerSide(pathOr(f.NewPath, f), f, rightW)
	}

	var b strings.Builder
	b.WriteString(m.styles.GrayFg(f.Path()))
	if f.Binary {
		b.WriteString(" ")
		b.WriteString(m.styles.DiffBinary().Render("binary"))
		return b.String()
	}
	b.WriteString(m.styles.GreenFg(fmt.Sprintf("  +%d", f.Additions)))
	b.WriteString(m.styles.RedFg(fmt.Sprintf(" -%d", f.Deletions)))
	return b.String()
}

// headerSide renders one column of the split header, truncated to its
// column and padded so the two halves line up.
func (m Model) headerSide(path string, f *diff.File, width int) string {
	stats := ""
	if !f.Binary {
		stats = m.styles.GreenFg(fmt.Sprintf("  +%d", f.Additions)) +
			m.styles.RedFg(fmt.Sprintf(" -%d", f.Deletions))
	} else {
		stats = " " + m.styles.DiffBinary().Render("binary")
	}

	pathBudget := width - ansi.StringWidth(stats) - 1
	if pathBudget < 1 {
		pathBudget = 1
	}

	var b strings.Builder
	b.WriteString(m.styles.GrayFg(tailTruncate(path, pathBudget)))
	b.WriteString(stats)
	out := ansi.Truncate(b.String(), max(1, width), "…")
	if pad := width - ansi.StringWidth(out); pad > 0 {
		out += strings.Repeat(" ", pad)
	}
	return out
}

func pathOr(path string, f *diff.File) string {
	if path != "" {
		return path
	}
	return f.Path()
}

// tailTruncate shortens a path from the left so the file name stays visible.
func tailTruncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	const ellipsis = "…"
	return ansi.TruncateLeft(s, ansi.StringWidth(s)-w+ansi.StringWidth(ellipsis), ellipsis)
}

// renderLine renders one full-width diff line with word-level highlights and
// search matches baked in. flatIdx is the line's index into the plain lines.
func (m Model) renderLine(row int, l diff.Line, flatIdx int) string {
	if l.Text == "" {
		return ""
	}
	base, out := m.styledText(row, l, flatIdx)
	return m.padLine(out, base)
}

// styledText renders one line's text, resolving token, word and search styles
// per boundary. Lines with nothing to highlight take the fast path.
func (m Model) styledText(row int, l diff.Line, flatIdx int) (lipgloss.Style, string) {
	base := m.lineStyles(l)
	if l.Text == "" {
		return base, ""
	}

	toks := m.tokens(row, l)
	spans := m.spansFor(flatIdx)
	words := l.WordRanges()
	if len(toks) == 0 && len(words) == 0 && len(spans) == 0 {
		return base, base.Render(l.Text)
	}

	var selected [2]int
	var hasSel bool
	if m.matchIdx >= 0 && m.matchIdx < len(m.matches) {
		sel := m.matches[m.matchIdx]
		if sel.Line == flatIdx {
			selected = [2]int{sel.Start, sel.End}
			hasSel = true
		}
	}

	kind := syntaxKind(l.Kind)
	bounds := tokenBounds(boundarySet(0, len(l.Text), words, spans), toks, len(l.Text))

	var b strings.Builder
	for i := 0; i+1 < len(bounds); i++ {
		a, z := bounds[i], bounds[i+1]
		if a == z {
			continue
		}
		mid := (a + z) / 2
		class := tokenClass(toks, mid)
		// Text no token owns keeps the line's own wash. The syntax table only
		// knows added, deleted and unchanged rows, so a hunk header or the
		// no-newline marker would otherwise lose its band to a context style.
		st := base
		if class != syntax.Plain {
			st = m.styles.Syntax(kind, class)
		}
		if containsSpan(words, mid) {
			st = m.styles.SyntaxWord(kind, class)
		}
		if containsMatch(spans, mid) {
			st = m.styles.SearchHi()
		}
		if hasSel && posInSpan(selected, mid) {
			st = m.styles.SearchHiSel()
		}
		b.WriteString(st.Render(l.Text[a:z]))
	}
	return base, b.String()
}

// syntaxKind maps a line kind to the row kind the token styles are indexed by.
func syntaxKind(k diff.LineKind) int {
	switch k {
	case diff.Addition:
		return styles.SyntaxAdd
	case diff.Deletion:
		return styles.SyntaxDel
	default:
		return styles.SyntaxContext
	}
}

// tokenBounds merges the edges of a line's tokens into a sorted boundary set.
// Token offsets are source offsets, so they move past the diff sign.
func tokenBounds(set []int, toks []uint32, n int) []int {
	if len(toks) == 0 {
		return set
	}
	for _, w := range toks {
		start, end, _ := syntax.UnpackToken(w)
		set = append(set, clampIndex(int(start)+1, n), clampIndex(int(end)+1, n))
	}
	slices.Sort(set)
	return slices.Compact(set)
}

// clampIndex keeps a boundary inside a line of n bytes.
func clampIndex(v, n int) int { return min(max(v, 0), n) }

// tokenClass is the class covering pos, or Plain where no token does. Tokens
// arrive in order, so the scan stops at the first one that starts after pos.
func tokenClass(toks []uint32, pos int) syntax.Class {
	for _, w := range toks {
		start, end, class := syntax.UnpackToken(w)
		if pos < int(start)+1 {
			return syntax.Plain
		}
		if pos < int(end)+1 {
			return class
		}
	}
	return syntax.Plain
}

// fileAtRow returns the file behind a document row, which decides the language.
// Only the all-changes view and a commit hold several files in one document.
// The commit view is one flat row slice, and the rows of it are told apart by
// the segments Aggregate recorded.
//
// rowSource is where a rendered row's tokens come from. A row belongs to a
// document, to the rows of that document written in one language, and to the
// path that names the language. Most documents are one file from top to bottom;
// a commit is one band of the aggregate per file.
type rowSource struct {
	file *diff.File
	path string
	from int
	to   int
}

// sourceAt is the source of document row i.
func (m Model) sourceAt(row int) rowSource {
	if m.doc != nil {
		f := m.doc.fileForRow(row)
		return singleSource(f)
	}
	f := m.file
	if f == nil {
		return rowSource{}
	}
	if segs := f.Segments; len(segs) > 0 {
		// A segment records the line its file starts at, but row is a screen
		// row, and split mode folds a run of replaced lines into max(dels,
		// adds) rows: from the first change on, every row sits above its line
		// and the arithmetic below would reach back into an earlier file.
		// Bands live in row space, and the aggregate writes one per file in
		// segment order, so they name the right file where lines cannot.
		var j int
		if len(m.fileBands) == len(segs) {
			j = sort.Search(len(m.fileBands), func(k int) bool { return m.fileBands[k] > row }) - 1
		} else {
			i := row - m.gut.header - len(m.noteRows)
			j = len(segs) - 1
			for j > 0 && segs[j].Row > i {
				j--
			}
			if i < segs[j].Row {
				j = -1
			}
		}
		if j >= 0 {
			to := f.NLines()
			if j+1 < len(segs) {
				to = segs[j+1].Row
			}
			return rowSource{file: f, path: segs[j].Path, from: segs[j].Row, to: to}
		}
	}
	return singleSource(f)
}

// singleSource is a whole file read in one language.
func singleSource(f *diff.File) rowSource {
	if f == nil {
		return rowSource{}
	}
	return rowSource{file: f, path: f.Path(), to: f.NLines()}
}

// syntaxMaxLine is the longest line worth colouring. A generated bundle or a
// minified file arrives as one enormous line, and past this length the tokens
// cost more to paint than they are worth reading, so the line keeps its diff
// wash alone.
const syntaxMaxLine = 400

// tokens returns the token run of a diff line, or nil when the file is not
// indexed and the line is painted with the diff wash alone.
func (m Model) tokens(row int, l diff.Line) []uint32 {
	if !m.syntaxOn || len(l.Text) > syntaxMaxLine {
		return nil
	}
	idx := m.indexFor(m.sourceAt(row))
	switch l.Kind {
	case diff.Addition:
		return idx.New(l.New)
	case diff.Deletion:
		return idx.Old(l.Old)
	case diff.Context:
		return idx.Old(l.Old)
	}
	return nil
}

// pruneSyntax drops the token indexes of files that are no longer on screen.
// They are keyed by the file they were built from, and every reload builds a new
// one, so an index kept past its document is weight nothing will ask for again.
// The caps bound the map already; this keeps it at the working set instead.
func (m *Model) pruneSyntax() {
	if len(m.syn) == 0 {
		return
	}
	for src := range m.syn {
		if src.file != m.file {
			delete(m.syn, src)
		}
	}
}

// indexFor returns the token index of a row's source, building it on first use.
// A diff laid out as a virtual document is never indexed: its rows are generated
// on demand, and tokenising millions of them whole would cost more than the diff
// did.
func (m Model) indexFor(src rowSource) *syntax.Index {
	if !m.syntaxOn || src.file == nil || src.file.NLines() > hugeFileLines {
		return nil
	}
	if idx, ok := m.syn[src]; ok {
		return idx
	}
	// A source with no language, or one whose tokens outgrew the budget, is
	// remembered as nil: a miss here costs a whole-file tokenisation, and one
	// per painted row would freeze the view.
	idx := buildIndex(src)
	if n := m.synCount(); n >= maxSyntaxFiles || m.synBytes()+idx.Bytes() > maxSyntaxTotal {
		clear(m.syn)
	}
	m.syn[src] = idx
	return idx
}

// synBytes is how much the token indexes hold together.
func (m Model) synBytes() int {
	n := 0
	for _, idx := range m.syn {
		n += idx.Bytes()
	}
	return n
}

// synCount is how many files hold an index, nil entries included: they cost a
// map slot each and are the ones most likely to be asked for again.
func (m Model) synCount() int {
	return len(m.syn)
}

// maxSyntaxFiles is how many files keep their tokens at once, and
// maxSyntaxBytes how much one index may take before it is dropped. Both are
// deliberately small: a diff cache that can hold 64 MB should not be shadowed
// by its highlighter.
const (
	maxSyntaxFiles = 16
	maxSyntaxBytes = 8 << 20
	maxSyntaxTotal = 32 << 20
)

// buildIndex tokenises every row of a diff once, keeping the state of each side
// so a comment or a string opened inside a hunk still colours what follows.
func buildIndex(src rowSource) *syntax.Index {
	lang := syntax.Detect(src.path, "")
	if lang == nil {
		return nil
	}
	b := syntax.NewBuilder(lang)
	for i := src.from; i < src.to && i < src.file.NLines(); i++ {
		l, ok := src.file.LineAt(i)
		if !ok || len(l.Text) < 2 {
			continue
		}
		text := l.Text[1:]
		switch l.Kind {
		case diff.Addition:
			b.New(text, l.New)
		case diff.Deletion:
			b.Old(text, l.Old)
		case diff.Context:
			b.Context(text, l.Old, l.New)
		}
	}
	idx := b.Index()
	if idx.Bytes() > maxSyntaxBytes {
		return nil
	}
	return idx
}

// renderSplitRow renders a pair of lines as two columns.
func (m Model) renderSplitRow(row int) string {
	return m.splitRow(row, m.rowBudget())
}

// splitRow renders one split-mode row into width cells, whatever kind it is: a
// pre-rendered header already fills the width, a full-width row (a hunk header)
// is one line, and everything else is a pair of columns.
func (m Model) splitRow(row, width int) string {
	if width <= 0 {
		return ""
	}
	r := m.rowAt(row)
	switch {
	case r.pre:
		return ansi.Truncate(r.text, width, "")
	case r.fullWidth:
		return m.renderLine(row, r.line, r.flat)
	}
	leftW := width / 2
	rightW := width - leftW
	return m.renderSide(row, r.left, r.leftFlat, r.hasLeft, leftW, true) +
		m.renderSide(row, r.right, r.rightFlat, r.hasRight, rightW, false)
}

// renderSide renders one column of a split row: line number, text, then a
// background wash out to the column edge.
func (m Model) renderSide(row int, l diff.Line, flatIdx int, present bool, colW int, oldSide bool) string {
	if colW <= 0 {
		return ""
	}
	if !present {
		return m.fillSide(colW)
	}

	base, text := m.styledText(row, l, flatIdx)

	var gut int
	var num string
	if m.showLineNumbers {
		gut = m.gut.numW + 1
		n := l.New
		if oldSide {
			n = l.Old
		}
		num = fmt.Sprintf("%*s ", m.gut.numW, lineNum(n))
	}

	contentW := max(1, colW-gut)
	// Scroll the text inside the column instead of truncating it: long lines
	// stay readable by moving the horizontal offset, and both columns move
	// together so the split stays aligned.
	x := m.vp.XOffset()
	text = ansi.Cut(text, x, x+contentW)
	line := num + text
	if pad := colW - ansi.StringWidth(line); pad > 0 {
		if hasBackground(base) {
			line += base.Render(strings.Repeat(" ", pad))
		} else {
			line += strings.Repeat(" ", pad)
		}
	}
	return line
}

// fillSide renders the blank half of a split row. A diagonal hatch keeps the
// row reading as one continuous band instead of leaving a hole in the layout,
// while the line-number gutter stays empty like a real missing line.
func (m Model) fillSide(colW int) string {
	if colW <= 0 {
		return ""
	}
	gut := 0
	if m.showLineNumbers {
		gut = m.gut.numW + 1
	}
	contentW := max(0, colW-gut)
	return strings.Repeat(" ", gut) + m.styles.DiffFill().Render(strings.Repeat("╱", contentW))
}

// hasBackground reports whether a style paints a background. lipgloss v2
// signals "none" with NoColor rather than a nil interface, so a nil check
// never fires.
func hasBackground(st lipgloss.Style) bool {
	_, none := st.GetBackground().(lipgloss.NoColor)
	return !none
}

// padLine extends a rendered line to the full content width with its line
// background, so diff washes and hunk header bands span the whole row like
// Zed and drift do. Padding is skipped when soft wrap is on, because
// trailing spaces would wrap onto empty continuation lines.
func (m Model) padLine(out string, base lipgloss.Style) string {
	if m.vp.SoftWrap || m.vpWidth() == 0 {
		return out
	}
	if !hasBackground(base) {
		return out
	}
	target := m.rowBudget()
	if m.showLineNumbers && !m.split {
		target -= m.gut.cells()
	}
	pad := target - lipgloss.Width(out)
	if pad <= 0 {
		return out
	}
	return out + base.Render(strings.Repeat(" ", pad))
}

func posInSpan(s [2]int, pos int) bool {
	return pos >= s[0] && pos < s[1]
}

func (m Model) lineStyles(l diff.Line) lipgloss.Style {
	switch l.Kind {
	case diff.Addition:
		return m.styles.DiffAdd()
	case diff.Deletion:
		return m.styles.DiffDel()
	case diff.HunkHeader:
		return m.styles.DiffHunk()
	case diff.NoNewline:
		return m.styles.DiffNoNewline()
	default:
		return m.styles.DiffContext()
	}
}

func containsSpan(spans []diff.Span, pos int) bool {
	for _, s := range spans {
		if pos >= s[0] && pos < s[1] {
			return true
		}
	}
	return false
}

func containsMatch(spans []Match, pos int) bool {
	for _, s := range spans {
		if pos >= s.Start && pos < s.End {
			return true
		}
	}
	return false
}

// boundarySet merges the highlight edges into sorted, unique slice points
// inside [lo, hi]. Edges are clamped: a span that came from somewhere other
// than this line (a stale index, an offset bug) must never produce an
// out-of-range slice, only a slightly wrong highlight.
func boundarySet(lo, hi int, words []diff.Span, matches []Match) []int {
	set := []int{lo, hi}
	clamp := func(v int) int { return min(max(v, lo), hi) }
	for _, s := range words {
		set = append(set, clamp(s[0]), clamp(s[1]))
	}
	for _, s := range matches {
		set = append(set, clamp(s.Start), clamp(s.End))
	}
	slices.Sort(set)
	set = slices.Compact(set)
	return set
}

// hasFileHeader reports whether the file header row is part of the layout.
func (m Model) hasFileHeader() bool { return m.file != nil && m.file.Path() != "" }

// updateGutterWidth sizes the number columns from the document's tallest line
// number, so the gutter never overflows into the content.
func (m *Model) updateGutterWidth() {
	lines := m.nlines()
	if m.doc != nil {
		// A virtual document only materialises part of its rows, so the
		// number columns must follow its full length.
		lines = max(lines, m.doc.lineCount())
	}
	digits := len(itoa(max(1, lines)))
	m.gut.numW = max(4, digits)
}

// numCells is the width of one number column, never below four digits.
func (g *gutterState) numCells() int { return max(4, g.numW) }

// cells is the total width of the two number columns and their gaps.
func (g *gutterState) cells() int { return 2*g.numCells() + 2 }

// gutterText renders the left line-number gutter. The state is read through a
// shared pointer, so the callback always sees the current file and mode.
func gutterText(g *gutterState, info viewport.GutterContext) string {
	if g.split {
		return ""
	}
	if info.Soft {
		return strings.Repeat(" ", g.cells())
	}
	if !g.show {
		return ""
	}
	if g.lineFor != nil {
		l, ok := g.lineFor(info.Index)
		if !ok {
			return blankGutterFor(g)
		}
		if l.Kind == diff.HunkHeader {
			label := "  ·    ·  "
			if pad := g.cells() - len(label); pad > 0 {
				return label + strings.Repeat(" ", pad)
			}
			return label
		}
		w := g.numCells()
		return fmt.Sprintf("%*s %*s ", w, lineNum(l.Old), w, lineNum(l.New))
	}
	if g.file == nil {
		return blankGutterFor(g)
	}
	// Index 0 onward is the notes block and file header; diff lines follow.
	idx := info.Index - g.header - g.notes
	if info.Index < 0 || idx < 0 {
		return blankGutterFor(g)
	}
	l, ok := g.file.LineAt(idx)
	if !ok {
		return blankGutterFor(g)
	}
	if l.Kind == diff.HunkHeader {
		label := "  ·    ·  "
		if pad := g.cells() - len(label); pad > 0 {
			return label + strings.Repeat(" ", pad)
		}
		return label
	}
	return fmt.Sprintf("%*s %*s ", g.numW, lineNum(l.Old), g.numW, lineNum(l.New))
}

// lineNum formats a line number, leaving absent sides (0) blank.
func lineNum(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func itoa(n int) string { return strconv.Itoa(n) }

func blankGutterFor(g *gutterState) string { return strings.Repeat(" ", g.cells()) }

// findAllFold returns byte offsets of all case-insensitive occurrences of
// sub in s.
func findAllFold(s, sub string) [][2]int {
	var matches [][2]int
	if sub == "" {
		return nil
	}
	from := 0
	for {
		i := indexOfFold(s, sub, from)
		if i < 0 {
			return matches
		}
		matches = append(matches, [2]int{i, i + len(sub)})
		from = i + len(sub)
	}
}

func indexOfFold(s, sub string, from int) int {
	if sub == "" || len(s) < len(sub) {
		return -1
	}
	for i := from; i <= len(s)-len(sub); i++ {
		if asciiEqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func asciiEqualFold(a, b string) bool {
	for i := 0; i < len(a); i++ {
		if lowerByte(a[i]) != lowerByte(b[i]) {
			return false
		}
	}
	return true
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// stripANSI removes escape sequences, used to derive plain text for search
// from pre-styled metadata lines.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// SetSmooth turns animated scrolling on or off at runtime, cancelling any
// animation in flight when it is turned off.
func (m *Model) SetSmooth(smooth bool) {
	m.smooth = smooth
	if !smooth {
		m.cancelAnim()
	}
}

// Smooth reports whether animated scrolling is on.
func (m Model) Smooth() bool { return m.smooth }

// isScrollInput reports whether the message is a discrete scroll step that
// should glide when smooth scrolling is on: a key or a wheel tick.
func isScrollInput(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseWheelMsg:
		return true
	}
	return false
}

// refreshNoteRows wraps the metadata notes to the viewport width, keeping the
// styled rows and their plain text index-aligned with the document rows. A
// note that fits stays a single row, so short headers cost nothing.
func (m *Model) refreshNoteRows() {
	// With soft wrap on the viewport wraps every row itself. Pre-wrapping here
	// too would wrap the same text twice and mangle the line.
	if m.wrap && !m.split {
		m.noteRows = m.noteRows[:0]
		m.notePlain = m.notePlain[:0]
		for _, n := range m.notes {
			m.noteRows = append(m.noteRows, n)
			m.notePlain = append(m.notePlain, stripANSI(n))
		}
		return
	}
	w := max(1, m.vpWidth())
	m.noteRows = m.noteRows[:0]
	m.notePlain = m.notePlain[:0]
	for _, n := range m.notes {
		if ansi.StringWidth(stripANSI(n)) <= w {
			m.noteRows = append(m.noteRows, n)
			m.notePlain = append(m.notePlain, stripANSI(n))
			continue
		}
		for _, line := range strings.Split(ansi.Wrap(n, w, " "), "\n") {
			m.noteRows = append(m.noteRows, line)
			m.notePlain = append(m.notePlain, stripANSI(line))
		}
	}
}
