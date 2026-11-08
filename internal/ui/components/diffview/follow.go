package diffview

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
)

// FollowEdit puts the cursor on a change that just arrived on disk and scrolls it
// into view a third of the way down, so the edit arrives in context.
func (m *Model) FollowEdit(line diff.Line) bool { return m.FollowEditIn("", line) }

// FollowEditIn is FollowEdit restricted to one file of a document. The line is
// matched by number first and text second, since text alone would land on the first
// identical "}" or "return".
func (m *Model) FollowEditIn(path string, line diff.Line) bool {
	if line.Kind != diff.Addition && line.Kind != diff.Deletion {
		return false
	}
	if row, ok := m.rowOfChange(path, line); ok {
		return m.followRow(row)
	}
	if row, ok := m.matchRow(path, line, true); ok {
		return m.followRow(row)
	}
	if row, ok := m.matchRow(path, line, false); ok {
		return m.followRow(row)
	}
	return false
}

// rowOfChange places a changed line without walking the document: the diff numbers
// the line and the layout says which row paints it. It answers false where rows are
// paired, since a split row is two lines and the scan is the only way to pick one.
// Wrapping is not such a case: it turns a row into several display lines, which the
// scroll target accounts for, not the row the line is on.
func (m *Model) rowOfChange(path string, line diff.Line) (int, bool) {
	if m.doc == nil || m.split {
		return 0, false
	}
	block := 0
	if !m.doc.single() {
		var ok bool
		if block, ok = m.blockOf(path); !ok {
			return 0, false
		}
	}
	f := m.doc.blockFile(block)
	if f == nil {
		return 0, false
	}
	at := f.RowOf(line)
	if at < 0 {
		return 0, false
	}
	row := m.doc.rowOfFileRow(block, at)
	if row < 0 || row >= m.logicalRows() {
		return 0, false
	}
	row = m.displayOfLogical(row)
	if row < 0 {
		return 0, false
	}
	// The block may have been evicted: ask for the row so it is there to paint.
	m.rowAt(row)
	return row, true
}

// blockOf names the block a path's diff occupies in a document of files.
func (m Model) blockOf(path string) (int, bool) {
	n := m.doc.nblocks()
	for i := 0; i < n; i++ {
		if f := m.doc.fileAt(i); f != nil && f.Path() == path {
			return i, true
		}
	}
	return 0, false
}

// matchRow finds the row holding the change, by number or by text.
func (m *Model) matchRow(path string, line diff.Line, byNumber bool) (int, bool) {
	match := func(l diff.Line) bool {
		if l.Kind != line.Kind {
			return false
		}
		if byNumber {
			if line.Kind == diff.Addition {
				return line.New != 0 && l.New == line.New
			}
			return line.Old != 0 && l.Old == line.Old
		}
		return l.Text == line.Text
	}
	if m.doc == nil {
		if m.file == nil {
			return 0, false
		}
		for i := range m.file.Lines {
			if match(m.file.Lines[i]) {
				return m.displayOfLogical(i + m.gut.header + len(m.noteRows)), true
			}
		}
		return 0, false
	}
	for row := 0; row < m.nrows(); row++ {
		if path != "" {
			f := m.rowFile(row)
			if f == nil || f.Path() != path {
				continue
			}
		}
		if m.eachLine(row, match) {
			return row, true
		}
	}
	return 0, false
}

// followRow places the cursor on a row with a third of the pane above it. It reports
// whether anything moved: a no-op must not cost the reader their anchor.
func (m *Model) followRow(row int) bool {
	if row < 0 || row >= m.nrows() {
		return false
	}
	top := max(0, min(row-m.vpHeight()/3, max(0, m.maxScroll())))
	if m.vp.SoftWrap {
		// The viewport counts wrapped display lines, so a row is not a line.
		top = max(0, m.displayLineOf(row)-max(1, m.vp.Height())/3)
	}
	if row == m.cursor && top == m.vp.YOffset() && m.vp.XOffset() == 0 {
		return false
	}
	// A mark set belongs to the file it was collected in.
	if m.HasCursor() && m.rowFile(row) != m.rowFile(m.cursor) {
		m.pending = stage.NewSelection()
		m.markPath = ""
	}
	m.CancelScroll()
	m.glideTo(top)
	// Show the line from its start, or a change at the right arrives off screen.
	m.vp.SetXOffset(0)
	// The cursor is placed even before the caller knows staging is on: a follow that
	// only scrolled would be overwritten by the next placement.
	m.cursor = row
	m.anchor = -1
	return true
}

// displayLine is where a row starts in the viewport's coordinates under soft wrap.
func (m Model) displayLine(row int) int {
	if !m.vp.SoftWrap {
		return row
	}
	width := max(1, m.vp.Width())
	lines := 0
	for i := 0; i < row && i < len(m.rendered); i++ {
		lines += max(1, (ansi.StringWidth(m.rendered[i])+width-1)/width)
	}
	return lines
}

// wrapWithBar paints the staging gutter over the wrapped window: the viewport wraps
// and scrolls in display lines, so the bar cannot live in the row cache.
func (m Model) wrapWithBar() string {
	lines := strings.Split(m.vp.View(), "\n")
	gutter := m.barWidth()
	cursorLine := -1
	if m.HasCursor() {
		cursorLine = m.displayLineOf(m.cursor) - m.vp.YOffset()
	}
	readLine := -1
	if row := m.readBarRow(); row >= 0 {
		readLine = max(0, m.displayLineOf(row)-m.vp.YOffset())
	}
	for i := range lines {
		bar := strings.Repeat(" ", gutter)
		switch i {
		case cursorLine:
			bar = m.barAt(m.cursor)
		case readLine:
			bar = m.styles.CursorBar().Render("┃")
		}
		lines[i] = bar + lines[i]
	}
	return strings.Join(lines, "\n")
}

// displayLineOf is displayLine with the prefix table, for the paint path.
func (m Model) displayLineOf(row int) int {
	if !m.vp.SoftWrap || row < 0 {
		return row
	}
	if row < len(m.wrapPrefix) {
		return m.wrapPrefix[row]
	}
	return m.displayLine(row)
}

// FollowEditInBand is FollowEditIn for one block, so a big document is a lookup
// instead of a crawl.
func (m *Model) FollowEditInBand(band int, path string, line diff.Line) bool {
	if line.Kind != diff.Addition && line.Kind != diff.Deletion {
		return false
	}
	if band < 0 || band >= len(m.fileBands) {
		return m.FollowEditIn(path, line)
	}
	if row, ok := m.rowOfChange(path, line); ok {
		return m.followRow(row)
	}
	start := max(0, m.fileBands[band])
	end := m.nrows()
	if band+1 < len(m.fileBands) {
		end = m.fileBands[band+1]
	}
	// A document materialises only a window of blocks: ask for one row to bring the
	// block back.
	m.rowAt(m.displayOfLogical(start))
	if row, ok := m.matchRowIn(start, end, path, line, true); ok {
		return m.followRow(row)
	}
	if row, ok := m.matchRowIn(start, end, path, line, false); ok {
		return m.followRow(row)
	}
	return false
}

// matchRowIn is matchRow over a row range.
func (m *Model) matchRowIn(start, end int, path string, line diff.Line, byNumber bool) (int, bool) {
	match := func(l diff.Line) bool {
		if l.Kind != line.Kind {
			return false
		}
		if byNumber {
			if line.Kind == diff.Addition {
				return line.New != 0 && l.New == line.New
			}
			return line.Old != 0 && l.Old == line.Old
		}
		return l.Text == line.Text
	}
	for row := max(0, start); row < min(end, m.nrows()); row++ {
		if path != "" {
			f := m.rowFile(row)
			if f == nil || f.Path() != path {
				continue
			}
		}
		if m.eachLine(row, match) {
			return row, true
		}
	}
	return 0, false
}

// SyncContent makes the viewport's line count exact, which pushContent skips while a
// document streams.
func (m *Model) SyncContent() {
	if len(m.blank) == 0 {
		return
	}
	m.blank = m.blank[:0]
	m.pushContent()
}

// ensureContent brings the viewport's line count up when an append left it behind.
func (m *Model) ensureContent() {
	if len(m.blank) == 0 || len(m.blank) >= m.nrows() {
		return
	}
	m.blank = m.blank[:0]
	m.pushContent()
}

// SetSide says which half of the file on screen this pane shows.
func (m *Model) SetSide(staged bool) { m.fileStaged = staged }

// BlockSide says which half of a file a block shows, and how its band names it.
type BlockSide struct {
	// Staged is the index side; the worktree side is the other one.
	Staged bool
	// Tag is appended to the band line, for the blocks of a doubled path.
	Tag string
}

// AppendBlocksSided adds blocks and the side each one shows: a file can appear twice,
// so the side belongs to the block rather than to the path.
func (m *Model) AppendBlocksSided(files []*diff.File, sides []BlockSide) {
	if m.doc == nil || len(files) == 0 {
		return
	}
	tags := make([]string, len(files))
	staged := make([]bool, len(files))
	for i := range files {
		if i < len(sides) {
			tags[i], staged[i] = sides[i].Tag, sides[i].Staged
		}
	}
	m.appendBlocks(files, tags)
	m.blockStaged = append(m.blockStaged, staged...)
}

// CursorStaged reports which half of a file the cursor is in.
func (m Model) CursorStaged() (bool, bool) {
	if m.doc == nil {
		return m.fileStaged, true
	}
	if !m.HasCursor() || len(m.blockStaged) == 0 {
		return false, false
	}
	band := sort.SearchInts(m.fileBands, m.cursor)
	if band >= len(m.fileBands) || m.fileBands[band] > m.cursor {
		band--
	}
	if band < 0 || band >= len(m.blockStaged) {
		return false, false
	}
	return m.blockStaged[band], true
}

// BandRow is where block i starts.
func (m Model) BandRow(i int) int {
	if i < 0 || i >= len(m.fileBands) {
		return -1
	}
	return m.fileBands[i]
}
