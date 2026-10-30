// Package diffview renders a single file's diff as a glow-style pager:
// full-bleed viewport, glow line numbers, and Zed/VS Code-style word-level
// diff highlighting baked into ANSI-rendered lines, with search matches
// layered on top. Diffs can be shown as one-column (unified) or as two
// side-by-side columns (split), and the pager draws its own vertical and
// horizontal scrollbars. Large scroll jumps animate with harmonica.
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
	"samouly.fun/twig/internal/ui/styles"
)

const (
	frameInterval = time.Second / 60
	// baseSpringFreq is how stiff the scroll spring is for a short step, in
	// radians per second. It rises with the distance still to travel, so a
	// line step glides gently while a page jump gets there quickly.
	baseSpringFreq = 26.0
	// maxSpringFreq caps that rise, keeping long jumps fast without turning
	// them into a cut.
	maxSpringFreq = 60.0
	// smallStepRows is the distance a scroll input may move before it is worth
	// animating. A line or a wheel tick lands immediately: gliding them makes
	// the view feel behind the keyboard.
	smallStepRows = 3
	// scrollbarSize is the space reserved on the right and at the bottom of
	// the pane for the scrollbars, in cells.
	scrollbarSize = 1
	// maxCachedRows bounds the memoised row cache; past it the cache stops
	// growing rather than holding a whole huge file.
	maxCachedRows = 1 << 16
)

// Match is a search match in rendered content coordinates.
type Match struct {
	Line       int
	Start, End int
}

// gutterState holds what the viewport gutter needs to know. It is shared by
// pointer with the viewport's gutter callback: the callback is captured once
// when the viewport is created, so it must never be bound to a Model copy
// (Model is passed by value all over Bubble Tea).
type gutterState struct {
	file  *diff.File
	notes int
	show  bool
	split bool
	// header is 1 when the file header row is rendered above the diff, 0 for
	// an aggregate document (a commit), where the path is empty.
	header int
	// numW is the cells reserved per side for a line number. It grows with
	// the digits of the longest line, so seven-digit numbers cannot push the
	// row wider than the gutter and wrap the pane.
	numW int
	// lineFor resolves a document row to its diff line for the virtual
	// multi-file view, where rows no longer map onto one file's lines.
	lineFor func(row int) (diff.Line, bool)
}

// contentRow is one rendered row. Unified mode uses one line per row; split
// mode pairs a deleted line with an added one.
type contentRow struct {
	// pre marks rows whose text is already rendered (notes, file header).
	pre  bool
	text string
	// fullWidth rows span both columns even in split mode (hunk headers,
	// "\ No newline" markers).
	fullWidth bool
	line      diff.Line
	flat      int
	hunk      bool

	left, right         diff.Line
	hasLeft, hasRight   bool
	leftFlat, rightFlat int
}

// rowCache memoises styled rows for one rendering mode, so a scroll frame only
// styles the rows entering the window. Model is passed by value all over
// Bubble Tea, so the cache is shared through a pointer.
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
	// doc is the virtual multi-file document, nil when a single file is
	// shown. It owns the row layout in that case; rows/plain stay empty.
	doc   *blockDoc
	gut   *gutterState
	cache *rowCache

	notes []string // metadata lines above the diff
	// noteRows is those lines wrapped to the viewport width, with notePlain
	// their searchable text. A long commit description takes as many rows as
	// it needs instead of being clipped.
	noteRows  []string
	notePlain []string
	rows      []contentRow
	indexed   int      // rows already folded into hunks, bands and width
	laid      int      // document lines already laid out as rows
	rendered  []string // ANSI-rendered content lines
	// blank is the row placeholder buffer handed to the viewport. It is
	// reused across rebuilds: a three-million row document must not allocate
	// sixteen megabytes every time the window resizes.
	blank     []string
	plain     []string // plain text, search coordinates
	hunks     []int    // rendered-line indexes of hunk headers
	fileBands []int    // rendered-line indexes of per-file bands
	// hunkBlock and hunkLocal map a global hunk index back to its block, so
	// the current hunk's patch can be rebuilt from a virtual document.
	hunkBlock []int32
	hunkLocal []int32
	// hunkCur is the hunk targeted by the last hunk jump, or -1.
	hunkCur int

	contentWidth int // widest rendered line, drives the horizontal scrollbar

	term     string
	matches  []Match
	matchIdx int
	// searchSeq numbers the running search so the background scan's results
	// can be dropped when it has been replaced or cleared.
	searchSeq int
	// folded is a lazily built, ASCII-lowercased mirror of plain. Folding
	// byte-for-byte keeps match offsets valid and lets search use
	// strings.Index instead of scanning character by character.
	folded []string

	smooth  bool
	spring  harmonica.Spring
	pos     float64
	vel     float64
	target  float64
	anim    bool
	animSeq int
	// lastFrame is when the previous animation frame ran, so the spring can
	// advance by wall-clock time instead of a fixed assumption.
	lastFrame time.Time

	split bool
	wrap  bool
	// rx is the compiled regular expression when the search term looks like
	// one, so patterns work without a separate mode.
	rx              *regexp.Regexp
	width, height   int
	showLineNumbers bool
}

// scrollFrameMsg drives smooth scroll animation frames. The sequence number
// makes stale frames from a superseded animation harmless: exactly one chain
// of ticks can be alive at a time. The timestamp lets the spring advance by
// the real elapsed time, so a slow frame stretches the delay but not the
// motion.
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
		smooth:          smooth,
		spring:          harmonica.NewSpring(frameInterval.Seconds(), baseSpringFreq, 1.0),
		showLineNumbers: true,
		hunkCur:         -1,
	}
}

// SetStyles updates the theme and re-renders the content.
func (m *Model) SetStyles(st styles.Styles) {
	m.styles = st
	m.rebuild()
}

// SetSize resizes the viewport. Scrollbars live inside the given box, so the
// viewport gets one column and one row less.
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

// bars reports whether the scrollbars are shown. Wrapped text never scrolls
// horizontally and already uses the full width, so bars are hidden there.
func (m Model) bars() bool { return !m.vp.SoftWrap && m.width >= 3 && m.height >= 3 }

func (m Model) vpWidth() int {
	if !m.bars() {
		return max(1, m.width)
	}
	return max(1, m.width-scrollbarSize)
}

func (m Model) vpHeight() int {
	if !m.bars() {
		return max(1, m.height)
	}
	// The bottom bar only takes a row when there is something to scroll
	// horizontally.
	if m.hscroll() {
		return max(1, m.height-scrollbarSize)
	}
	return max(1, m.height)
}

// hscroll reports whether the content is wider than the viewport, i.e.
// whether the horizontal scrollbar is needed at all.
func (m Model) hscroll() bool {
	return m.bars() && m.contentWidth > m.vpWidth()
}

// SetWrap toggles soft wrapping. Wrapping is disabled in split mode, where
// the two columns already make lines narrow.
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

// hugeFileLines is where a single diff switches to the virtual layout. Below
// it the flat row slice is cheaper than the indirection.
const hugeFileLines = 50_000

// setFlat lays the diff out as one flat row slice, the layout used for every
// ordinary file and the reference the virtual layout has to match.
func (m *Model) setFlat(f *diff.File) {
	m.doc = nil
	m.gut.lineFor = nil
	m.file = f
	m.gut.file = f
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.hunkCur = -1
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
// materialises block i's diff on demand: only a small window of blocks keeps
// its rows, every other block stays layout only.
func (m *Model) SetDocument(load func(i int) *diff.File) {
	m.doc = newBlockDoc(load)
	m.gut.file = nil
	m.gut.lineFor = m.doc.lineForRow
	m.file = nil
	m.term = ""
	m.matches = nil
	m.matchIdx = -1
	m.hunkCur = -1
	m.cancelAnim()
	m.rows = m.rows[:0]
	m.plain = m.plain[:0]
	m.rendered = nil
	m.hunks = m.hunks[:0]
	m.fileBands = m.fileBands[:0]
	m.hunkBlock = m.hunkBlock[:0]
	m.hunkLocal = m.hunkLocal[:0]
	m.doc.setMode(m.split)
	m.rebuildDoc()
	m.vp.GotoTop()
}

// AppendBlocks adds the next files to the virtual document. Their layout is
// recorded immediately, their rows only while they stay in the view window.
func (m *Model) AppendBlocks(files []*diff.File) {
	if m.doc == nil || len(files) == 0 {
		return
	}
	m.refreshNoteRows()
	pre := len(m.noteRows)
	if m.hasFileHeader() {
		pre++
	}
	m.doc.add(files, pre)
	m.doc.setMode(m.split)
	m.rebuildNav()
	m.rebuildDoc()
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
		m.fileBands = append(m.fileBands, start)
	}
}

// rebuildDoc refreshes everything derived from the document layout: the hunk
// and band lists, the content width and the row count.
func (m *Model) rebuildDoc() {
	m.cache.reset()
	m.updateGutterWidth()
	m.doc.setGutter(m.gut.cells(), m.gut.numCells()+1)
	m.contentWidth = m.doc.width(m.vpWidth(), m.split, m.showLineNumbers)
	m.pushContent()
}

// DocBlocks reports how many blocks a virtual document holds and how many of
// them currently carry rows.
func (m Model) DocBlocks() (blocks, materialised int) {
	if m.doc == nil {
		return 0, 0
	}
	return m.doc.nblocks(), m.doc.materialised()
}

// Update processes animation frames and forwards everything else to the
// viewport. With smooth scrolling on, a discrete step (a scroll key or a wheel
// tick) moves the animation target instead of the viewport, so every way of
// moving through a diff glides and repeated presses accumulate instead of
// restarting from wherever the spring happened to be.
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

	if !m.smooth || !scroll || after == before {
		return m, cmd
	}

	step := float64(after - before)
	if m.anim {
		// Retarget the running animation: the target collects the steps while
		// the viewport keeps following the spring. The step is undone on the
		// viewport so the frame does not jump ahead of the spring.
		m.target = m.clampTarget(m.target + step)
		m.vp.SetYOffset(before)
		return m, cmd
	}
	if math.Abs(step) < smallStepRows {
		// Arrow keys and wheel ticks land now instead of gliding; the
		// viewport already moved, so there is nothing to do.
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

// View renders the visible window with its scrollbars. Painting is
// O(viewport), not O(file): only the rows in view are styled, which keeps
// navigation instant on diffs with hundreds of thousands of lines.
func (m Model) View() string {
	if !m.bars() {
		return m.vp.View()
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

// SearchResultsMsg carries the complete match list of a background search.
// Seq identifies the search it belongs to, so results from a superseded or
// cancelled search are recognised and dropped instead of painted.
type SearchResultsMsg struct {
	Seq     int
	Term    string
	Matches []Match
}

// Search starts a two-phase search: the rows in view are scanned first, so
// highlights appear on the current screen without waiting, and the whole
// document is then scanned in the background, delivered as SearchResultsMsg.
// It returns how many matches the first phase found and the command that
// finishes the scan.
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
// scans its own sources, which is what keeps a search over millions of rows
// cheap; everything else folds line by line.
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

// sourceMatches finds term in document lines [from, to) of a single paged file
// document by searching the diff's own sources. The rows above the document are
// scanned line by line; the diff rows come from the sources, mapped back to
// document lines. It reports false when the document cannot answer that way, or
// when the term needs the rendered text.
func (m Model) sourceMatches(term string, from, to int) ([]Match, bool) {
	if m.doc == nil || m.doc.src == nil || !m.doc.src.Paged() {
		return nil, false
	}
	return sourceMatches(m.doc.src, len(m.doc.pre), m.doc.preTxt, m.lineBase(), term, from, to)
}

// sourceMatches is the scan behind sourceMatches, split out so a background scan
// can hold only the values it needs and never touch the model.
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

// lineBase converts a document line index into the flat index the rows use.
// A flat document's plain slice already includes the notes and the header, so
// the two coincide; a virtual document indexes lines from zero, so its rows
// start len(notes)+1 further along.
func (m Model) lineBase() int {
	if m.doc != nil {
		return m.doc.flatBase()
	}
	return 0
}

// visibleLineRange is the span of lines the first phase scans: whatever the
// rows in view cover.
func (m Model) visibleLineRange() (int, int) {
	if m.doc != nil {
		return m.doc.lineRange(m.vp.YOffset(), m.vpHeight())
	}
	from := max(0, m.vp.YOffset())
	return from, min(m.nlines(), from+m.vpHeight())
}

// foldedFor returns the lowercase mirror when one is built for a flat
// document. A virtual document has no mirror and folds line by line.
func (m Model) foldedFor() []string {
	if m.doc != nil {
		return nil
	}
	return m.folded
}

// ApplySearchResults installs the completed match list and jumps to the first
// match at or below the current position. It reports false when the results
// belong to a search that has been replaced or cleared.
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

// ensureFolded builds the ASCII-lowercase mirror used by the fast scanner when
// it is missing or stale.
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

// scanLines collects the matches of term in lines[from:to]. text returns the
// text of a line; folded is an optional lowercase mirror of the whole document
// (nil means fold each line as it is scanned, which is what a virtual document
// needs since it does not keep every line in memory). lineBase is added to the
// line index so the matches come back in row coordinates.
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
			// Flat document: the mirror is built once, so every line is a
			// plain string search.
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

		// Virtual document: no mirror, so fold into a scratch buffer and
		// search it. This keeps a scan of millions of lines allocation-free.
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

// searchRegexp compiles term as a case-insensitive pattern when it contains
// anything that reads as a regular expression. A plain word stays a plain
// substring search, which is what the fast paths are built for. A pattern that
// does not compile falls back to the literal search instead of failing.
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

// indexAll recomputes the match list for the active term. It never scrolls, so
// a document can grow while a search is running without moving the view.
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

// foldASCII lowers ASCII letters without changing byte offsets, so spans map
// back onto the original text.
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
	m.vp.EnsureVisible(m.rowForLine(mt.Line), mt.Start, mt.End)
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

// rowForLine maps a match's flat line index onto the row that paints it. In a
// flat document the two coincide; a virtual document has no notes or header
// row, and its blocks are prefix-summed, so the mapping has to be asked for.
func (m Model) rowForLine(flat int) int {
	if m.doc != nil {
		return m.doc.rowForLine(flat - m.lineBase())
	}
	return flat
}

// ScrollTo scrolls smoothly (or directly) to the given rendered-line offset.
// The returned command drives the animation when smooth scrolling is on.
func (m *Model) ScrollTo(line int) tea.Cmd {
	line = max(0, min(line, m.maxScroll()))
	if !m.smooth {
		m.vp.SetYOffset(line)
		return nil
	}
	m.pos = float64(m.vp.YOffset())
	m.vel = 0
	m.target = float64(line)
	if math.Abs(m.target-m.pos) < smallStepRows {
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

// JumpTo scrolls immediately, cancelling any animation in flight. Selection
// driven navigation uses this so holding an arrow key never leaves the pane
// easing after the key is released.
func (m *Model) JumpTo(line int) {
	m.cancelAnim()
	m.vp.SetYOffset(max(0, line))
}

// Anchor returns where the reader is: the flat line at the top of the
// viewport. A refresh that replaces the content with SetFileAt puts them back
// there, so saving a file does not throw away their place in a long diff. A
// flat document has no line index, so plain rows serve, and JumpToLine hands
// the value straight back.
func (m Model) Anchor() int {
	if m.doc != nil {
		return m.doc.lineAtRow(m.vp.YOffset()) + m.lineBase()
	}
	return m.vp.YOffset()
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

// GotoBottom scrolls to the end of the diff.
func (m *Model) GotoBottom() tea.Cmd {
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

func (m *Model) cancelAnim() {
	m.anim = false
	m.vel = 0
	m.lastFrame = time.Time{}
	m.animSeq++
}

func (m *Model) updateScroll(now time.Time) tea.Cmd {
	if !m.anim {
		return nil
	}

	// Advance by the time that actually elapsed, so a slow frame delays the
	// motion instead of stretching it. A long gap (the app was suspended)
	// is capped: catching up should not fling the view.
	dt := now.Sub(m.lastFrame)
	if m.lastFrame.IsZero() || dt <= 0 {
		dt = frameInterval
	}
	dt = max(frameInterval/4, min(dt, 50*time.Millisecond))
	m.lastFrame = now

	m.target = max(0, min(m.target, float64(m.maxScroll())))
	// Stiffness grows with the distance left, so a one-line step stays soft
	// and a long jump does not crawl. Rebuilt per frame because the spring
	// bakes its timestep into its coefficients.
	freq := baseSpringFreq + math.Min(math.Abs(m.target-m.pos)*0.6, maxSpringFreq-baseSpringFreq)
	m.spring = harmonica.NewSpring(dt.Seconds(), freq, 1.0)
	m.pos, m.vel = m.spring.Update(m.pos, m.vel, m.target)

	if math.Abs(m.target-m.pos) < 0.02 && math.Abs(m.vel) < 0.5 {
		m.pos, m.vel, m.anim = m.target, 0, false
		m.vp.SetYOffset(int(math.Round(m.target)))
		return nil
	}
	m.vp.SetYOffset(int(math.Round(m.pos)))
	return tickScroll(m.animSeq)
}

func tickScroll(seq int) tea.Cmd {
	return tea.Tick(frameInterval, func(at time.Time) tea.Msg {
		return scrollFrameMsg{seq: seq, at: at}
	})
}

// NextHunk scrolls to the next hunk header, if any. The first press from
// above the first hunk lands on that hunk; later presses step through the
// rest.
func (m *Model) NextHunk() tea.Cmd {
	next := m.CurrentHunkIndex() + 1
	if next < 0 || next >= len(m.hunks) {
		return nil
	}
	m.hunkCur = next
	return m.ScrollTo(m.hunks[next])
}

// PrevHunk scrolls to the previous hunk header, if any.
func (m *Model) PrevHunk() tea.Cmd {
	cur := m.CurrentHunkIndex()
	if cur > 0 {
		m.hunkCur = cur - 1
		return m.ScrollTo(m.hunks[cur-1])
	}
	m.hunkCur = 0
	return m.ScrollTo(0)
}

// JumpToBand scrolls smoothly to the per-file band with the given index.
func (m *Model) JumpToBand(i int) tea.Cmd {
	if i < 0 || i >= len(m.fileBands) {
		return nil
	}
	return m.ScrollTo(m.fileBands[i])
}

// JumpToBandNow jumps to the per-file band immediately. Sidebar navigation
// uses this so rapid selection changes never queue animations.
func (m *Model) JumpToBandNow(i int) {
	if i < 0 || i >= len(m.fileBands) {
		return
	}
	m.hunkCur = -1
	m.JumpTo(m.fileBands[i])
}

// HasBand reports whether the i-th per-file band has been laid out. A document
// that streams in only knows its bands up to the last block received.
func (m Model) HasBand(i int) bool { return i >= 0 && i < len(m.fileBands) }

// CurrentHunkIndex returns the index of the hunk currently in view, or -1.
// A hunk targeted by NextHunk/PrevHunk wins while its header is still on
// screen, so the last hunk in a short file (which cannot scroll to the top)
// is still the one reported.
func (m Model) CurrentHunkIndex() int {
	cur := m.vp.YOffset()
	if m.hunkCur >= 0 && m.hunkCur < len(m.hunks) {
		h := m.hunks[m.hunkCur]
		if h >= cur && h < cur+m.vpHeight() {
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

// buildRows lays out the content rows and their searchable plain text for
// the current rendering mode.
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

// appendRows lays out the document lines from index from onwards, keeping the
// rows already built for the lines before it. flat indexes stay stable because
// the sign-inclusive prefix is constant.
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

// splitRows pairs deletion and addition runs into side-by-side rows, leaving
// context lines on both sides and keeping hunk headers full width.
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

// setContent lays out the rows for the current mode and refreshes the
// viewport index.
func (m *Model) setContent() {
	m.refreshNoteRows()
	if m.doc != nil {
		m.rebuild()
		return
	}
	m.buildRows()
	m.rebuild()
}

// rebuild refreshes the search index, navigation stops, content width and the
// viewport's line count. In wrap mode it renders every row, because wrapped
// height depends on the text; otherwise the viewport only needs the count and
// rows are styled lazily in View.
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
	m.contentWidth = 0
	m.indexed = 0
	m.indexRows()
	m.pushContent()
}

// indexRows folds the rows laid out since the last pass into the navigation
// stops and the content width. Appending blocks to a document only adds rows
// at the end, so the extension path runs this over the tail alone.
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

// pushContent hands the row count to the viewport. Non-wrapped mode feeds
// blank lines and styles the window on demand.
func (m *Model) pushContent() {
	if m.vp.SoftWrap {
		n := m.nrows()
		rendered := make([]string, n)
		for i := 0; i < n; i++ {
			rendered[i] = m.renderRow(m.rowAt(i))
		}
		m.rendered = rendered
		m.vp.SetContentLines(rendered)
		for _, l := range rendered {
			m.contentWidth = max(m.contentWidth, ansi.StringWidth(l))
		}
		return
	}

	m.rendered = nil
	// The viewport only needs the line count for scrolling; View styles the
	// window itself. The first line carries the content width so the
	// viewport still clamps horizontal scrolling correctly, and it is never
	// painted in this mode.
	n := m.nrows()
	if cap(m.blank) < n {
		m.blank = make([]string, n)
	}
	m.blank = m.blank[:n]
	clear(m.blank)
	if n > 0 && m.contentWidth > m.vpWidth() {
		m.blank[0] = strings.Repeat(" ", m.contentWidth)
	}
	m.vp.SetContentLines(m.blank)
	// The horizontal bar only takes a row when it is needed, so the viewport
	// height has to follow both the size and the content width.
	m.vp.SetWidth(m.vpWidth())
	m.vp.SetHeight(m.vpHeight())
}

// ExtendFile swaps in the same document with more blocks appended at the end,
// keeping the rows already laid out, the scroll position and the active
// search. A document that streams in therefore never jumps under the reader.
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
// horizontal scrollbar. Styling never changes cell width. In split mode the
// two columns stay fixed and the text inside each column scrolls, so the
// reported width is the viewport plus the longest overflow of either side.
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
			line = m.paintRow(i, w)
			if len(m.cache.rows) < maxCachedRows {
				m.cache.rows[i] = line
			}
		}
		out = append(out, line)
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

// paintRow styles, clips and pads one row at its place in the window. The
// result depends only on the row, the viewport width and the horizontal
// offset, which is what the row cache keys on.
func (m Model) paintRow(i, w int) string {
	gutW := 0
	if m.showLineNumbers && !m.split {
		gutW = m.gut.cells()
	}
	contentW := max(0, w-gutW)
	x := m.vp.XOffset()

	text := m.renderRow(m.rowAt(i))
	switch {
	case m.split:
		// Each column already scrolled its own text; only oversized
		// full-width rows (hunk headers) need clipping.
		if ansi.StringWidth(text) > w {
			text = ansi.Cut(text, 0, w)
		}
	case x > 0:
		text = ansi.Cut(text, x, x+contentW)
	case ansi.StringWidth(text) > contentW:
		text = ansi.Cut(text, 0, contentW)
	}

	line := gutterText(m.gut, viewport.GutterContext{Index: i, TotalLines: m.nrows()}) + text
	if pad := w - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}

// spansFor returns the match run of one line. matches are kept sorted by line,
// so painting a row costs a binary search instead of a map with an entry per
// match (which is millions of entries on a big document).
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

func (m Model) renderRow(r contentRow) string {
	switch {
	case r.pre:
		return r.text
	case r.fullWidth:
		return m.renderLine(r.line, r.flat)
	default:
		return m.renderSplitRow(r)
	}
}

func (m Model) renderHeader(f *diff.File) string {
	if m.split {
		// Split mode gets a header per column: old path on the left, new
		// path on the right, each with the change counts.
		w := m.vpWidth()
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
func (m Model) renderLine(l diff.Line, flatIdx int) string {
	if l.Text == "" {
		return ""
	}
	base, out := m.styledText(l, flatIdx)
	return m.padLine(out, base)
}

// styledText renders one line's text, resolving word and search highlight
// styles per boundary. Lines without any highlight take the fast path.
func (m Model) styledText(l diff.Line, flatIdx int) (lipgloss.Style, string) {
	base, word := m.lineStyles(l)
	if l.Text == "" {
		return base, ""
	}

	spans := m.spansFor(flatIdx)
	words := l.WordRanges()
	if len(words) == 0 && len(spans) == 0 {
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

	bounds := boundarySet(0, len(l.Text), words, spans)

	var b strings.Builder
	for i := 0; i+1 < len(bounds); i++ {
		a, z := bounds[i], bounds[i+1]
		if a == z {
			continue
		}
		mid := (a + z) / 2
		st := base
		if containsSpan(words, mid) {
			st = word
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

// renderSplitRow renders a pair of lines as two columns.
func (m Model) renderSplitRow(r contentRow) string {
	w := m.vpWidth()
	leftW := w / 2
	rightW := w - leftW
	return m.renderSide(r.left, r.leftFlat, r.hasLeft, leftW, true) +
		m.renderSide(r.right, r.rightFlat, r.hasRight, rightW, false)
}

// renderSide renders one column of a split row: line number, text, then a
// background wash out to the column edge.
func (m Model) renderSide(l diff.Line, flatIdx int, present bool, colW int, oldSide bool) string {
	if colW <= 0 {
		return ""
	}
	if !present {
		return m.fillSide(colW)
	}

	base, text := m.styledText(l, flatIdx)

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
	target := m.vpWidth()
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

func (m Model) lineStyles(l diff.Line) (base, word lipgloss.Style) {
	switch l.Kind {
	case diff.Addition:
		return m.styles.DiffAdd(), m.styles.DiffAddWord()
	case diff.Deletion:
		return m.styles.DiffDel(), m.styles.DiffDelWord()
	case diff.HunkHeader:
		return m.styles.DiffHunk(), m.styles.DiffHunk()
	case diff.NoNewline:
		return m.styles.DiffNoNewline(), m.styles.DiffNoNewline()
	default:
		return m.styles.DiffContext(), m.styles.DiffContext()
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

// gutterText renders the left line-number gutter. The state is read through
// a shared pointer so the callback always sees the current file and mode.
// updateGutterWidth sizes the number columns from the document's tallest line
// number, so the gutter never overflows into the content.
// hasFileHeader reports whether the file header row is part of the layout.
func (m Model) hasFileHeader() bool { return m.file != nil && m.file.Path() != "" }

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
