package diffview

import (
	"sort"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
)

// ItemKind names what the staging cursor sits on, which is what a keystroke acts
// on: one verb covers a file, a hunk and a line, with no mode to be in.
type ItemKind uint8

// The things the cursor can rest on.
const (
	// ItemNone is a row nothing can act on: context, a note, an end-of-file marker.
	ItemNone ItemKind = iota
	// ItemFile is the file's own header row.
	ItemFile
	// ItemHunk is a hunk header.
	ItemHunk
	// ItemLine is an added or a deleted line.
	ItemLine
)

// Item is the object under the cursor.
type Item struct {
	Kind ItemKind
	// Hunk is the file-local hunk index for a line, the document-wide one for a hunk
	// header: a hunk is always resolved through its rows, never by the index.
	Hunk int
	// Line is set when Kind is ItemLine.
	Line diff.Line
}

// barWidth is the columns the staging gutter reserves. Reserving rather than
// inserting them keeps the code from shifting sideways when the cursor moves.
func (m Model) barWidth() int {
	if !m.selectable {
		return 0
	}
	return 2
}

// rowFile is the file a row belongs to: the one on screen, or the block's own in
// a document.
func (m Model) rowFile(row int) *diff.File {
	if m.doc != nil {
		return m.doc.fileForRow(row)
	}
	return m.file
}

// CursorFile is the file the cursor's rows belong to.
func (m Model) CursorFile() (string, bool) {
	if !m.HasCursor() {
		return "", false
	}
	if f := m.rowFile(m.cursor); f != nil {
		return f.Path(), true
	}
	return "", false
}

// CursorDiffFile is the diff the cursor's rows belong to, for a discard preview.
func (m Model) CursorDiffFile() *diff.File {
	if !m.HasCursor() {
		return nil
	}
	return m.rowFile(m.cursor)
}

// isBandRow reports whether a row is a file's separator band.
func (m Model) isBandRow(row int) bool {
	if m.doc == nil {
		return row >= 0 && row == m.fileHeaderRow()
	}
	i := sort.SearchInts(m.fileBands, row)
	return i < len(m.fileBands) && m.fileBands[i] == row
}

// SetSelectable turns the staging cursor on or off. A virtual document keeps the
// plain reader.
func (m *Model) SetSelectable(on bool) {
	if m.selectable == on {
		return
	}
	m.selectable = on
	if !on {
		m.cursor, m.anchor = -1, -1
		m.pending = stage.NewSelection()
	}
	// The gutter is part of the width now, so a pane that gains or loses the
	// cursor has to re-wrap around it.
	if m.vp.SoftWrap {
		m.applyWrap()
		m.rebuild()
	}
}

// Selectable reports whether the staging cursor is available.
func (m Model) Selectable() bool { return m.selectable }

// SetSelectMode switches between applying each keystroke at once and marking
// lines to apply together.
func (m *Model) SetSelectMode(on bool) { m.selectMode = on }

// SelectMode reports which apply model is in force.
func (m Model) SelectMode() bool { return m.selectMode }

// CursorRow is the row the cursor sits on, or -1.
func (m Model) CursorRow() int { return m.cursor }

// HasCursor reports whether the cursor has been placed.
func (m Model) HasCursor() bool { return m.cursor >= 0 && m.cursor < m.nrows() }

// SetCursorFocus says whether this pane holds the keys; the bar dims, never moves.
func (m *Model) SetCursorFocus(on bool) { m.cursorOn = on }

// CursorOn reports whether the bar is drawn bright.
func (m Model) CursorOn() bool { return m.cursorOn }

// resetCursor drops the cursor when the document behind it changes.
func (m *Model) resetCursor() {
	m.cursor, m.anchor = -1, -1
	m.pending = stage.NewSelection()
	m.markPath = ""
}

// ClearCursor drops the cursor before the document is replaced.
func (m *Model) ClearCursor() { m.resetCursor() }

// linesAt returns the diff lines a row holds: split mode pairs two.
func (m Model) linesAt(row int) []diff.Line {
	if row < 0 || row >= m.nrows() {
		return nil
	}
	r := m.rowAt(row)
	if r.pre {
		return nil
	}
	if m.split {
		var out []diff.Line
		if r.hasLeft {
			out = append(out, r.left)
		}
		if r.hasRight {
			out = append(out, r.right)
		}
		return out
	}
	return []diff.Line{r.line}
}

// hunkAtRow reports the file-local hunk whose header is on this row.
func (m Model) hunkAtRow(row int) (int, bool) {
	i := sort.SearchInts(m.hunks, row)
	if i < len(m.hunks) && m.hunks[i] == row {
		return i, true
	}
	return -1, false
}

// hunkRows is the inclusive row span of hunk h, including its header.
func (m Model) hunkRows(h int) (start, end int) {
	if h < 0 || h >= len(m.hunks) {
		return 0, 0
	}
	start = m.hunks[h]
	end = m.nrows() - 1
	if h+1 < len(m.hunks) {
		end = m.hunks[h+1] - 1
	}
	return start, end
}

// fileHeaderRow is the row holding the file's own header, or -1.
func (m Model) fileHeaderRow() int {
	if !m.hasFileHeader() || m.doc != nil {
		return -1
	}
	return len(m.noteRows)
}

// itemAt resolves a row to the object a keystroke would act on.
func (m Model) itemAt(row int) Item {
	if row < 0 || row >= m.nrows() {
		return Item{}
	}
	// A band and a hunk header are named by the layout, so they need no rows. Asking
	// a row for its file first would hide every row of an evicted block.
	if m.isBandRow(row) {
		return Item{Kind: ItemFile, Hunk: -1}
	}
	if h, ok := m.hunkAtRow(row); ok {
		return Item{Kind: ItemHunk, Hunk: h}
	}
	f := m.rowFile(row)
	if f == nil {
		return Item{}
	}
	for _, l := range m.linesAt(row) {
		if l.Kind != diff.Addition && l.Kind != diff.Deletion {
			continue
		}
		key, ok := stage.LineOf(l)
		if !ok {
			continue
		}
		return Item{Kind: ItemLine, Hunk: stage.HunkOf(f, key), Line: l}
	}
	return Item{}
}

// CursorItem is the object under the cursor.
func (m Model) CursorItem() Item { return m.itemAt(m.cursor) }

// nextItemRow finds the next actionable row in a direction, or -1.
func (m Model) nextItemRow(from, dir int) int {
	for row := from + dir; row >= 0 && row < m.nrows(); row += dir {
		if m.itemAt(row).Kind != ItemNone {
			return row
		}
	}
	return -1
}

// firstItemRow finds the first actionable row at or after from.
func (m Model) firstItemRow(from int) int {
	for row := max(0, from); row < m.nrows(); row++ {
		if m.itemAt(row).Kind != ItemNone {
			return row
		}
	}
	for row := max(0, from) - 1; row >= 0; row-- {
		if m.itemAt(row).Kind != ItemNone {
			return row
		}
	}
	return -1
}

// MoveCursor moves the cursor by delta actionable rows and drops any range. At the
// end of the diff it scrolls instead, so an arrow key is never dead.
func (m *Model) MoveCursor(delta int) bool {
	if !m.Selectable() || m.nrows() == 0 {
		return false
	}
	m.ClearRange()
	if m.cursor < 0 {
		return m.setCursor(m.firstItemRow(m.vp.YOffset()))
	}
	dir := 1
	if delta < 0 {
		dir, delta = -1, -delta
	}
	next := m.cursor
	moved := false
	for i := 0; i < delta; i++ {
		n := m.nextItemRow(next, dir)
		if n < 0 {
			// Nothing further that way: stop on the last item, or scroll if it could not
			// move at all.
			if !moved {
				return m.scrollPastEdge(dir)
			}
			break
		}
		next, moved = n, true
	}
	if !moved {
		return false
	}
	return m.setCursor(next)
}

// ExtendCursor moves the cursor and keeps the anchor. It stops at the ends of the
// document rather than scrolling: a range whose far end the reader cannot see is
// not one they can check.
func (m *Model) ExtendCursor(delta int) bool {
	if !m.Selectable() || m.nrows() == 0 || delta == 0 {
		return false
	}
	if !m.HasCursor() {
		if !m.PlaceCursor() {
			return false
		}
	}
	if m.anchor < 0 {
		m.anchor = m.cursor
	}
	dir, step := 1, delta
	if delta < 0 {
		dir, step = -1, -delta
	}
	// A range is one file's lines, not files.
	file := m.rowFile(m.anchor)
	next := m.cursor
	for i := 0; i < step; i++ {
		n := m.nextItemRow(next, dir)
		if n < 0 || m.rowFile(n) != file {
			break
		}
		next = n
	}
	return m.setCursor(next)
}

// StartRange anchors a range at the cursor and reports whether one is active.
func (m *Model) StartRange() bool {
	if !m.Selectable() || !m.HasCursor() {
		return false
	}
	m.anchor = m.cursor
	return true
}

// ClearRange drops an active range, keeping the cursor where it is.
func (m *Model) ClearRange() { m.anchor = -1 }

// RangeActive reports whether a range is anchored, even before it moves.
func (m Model) RangeActive() bool { return m.anchor >= 0 }

// HasRange reports whether more than one row is under selection.
func (m Model) HasRange() bool { return m.anchor >= 0 && m.anchor != m.cursor }

// scrollPastEdge scrolls one row when the cursor has nowhere left to go.
func (m *Model) scrollPastEdge(dir int) bool {
	before := m.vp.YOffset()
	if dir > 0 {
		m.vp.ScrollDown(1)
	} else {
		m.vp.ScrollUp(1)
	}
	return m.vp.YOffset() != before
}

// CursorAnchor is where the cursor was, in a form that survives a reload.
type CursorAnchor struct {
	// Path is the file the cursor was in.
	Path string
	// Line is the change under the cursor, if it was on one.
	Line stage.Line
	// HasLine says whether Line was set.
	HasLine bool
	// Row is the row it sat on, used when the line is gone.
	Row int
	// Top is the diff line at the top of the viewport: restoring it keeps the screen
	// still when staging removes rows above the reader.
	Top int
}

// AnchorCursor records where the reader is: the top of the viewport and the
// change under the cursor.
func (m Model) AnchorCursor(path string) CursorAnchor {
	a := CursorAnchor{Path: path, Row: m.cursor, Top: m.TopLine()}
	if m.cursor < 0 {
		return a
	}
	if l, ok := stage.LineOf(m.CursorItem().Line); ok {
		a.Line, a.HasLine = l, true
	}
	return a
}

// PlaceCursor puts the cursor on the first change on screen, so a freshly opened
// file shows where the next keystroke lands. A change beats the file header, which
// acts on the whole file.
func (m *Model) PlaceCursor() bool {
	if !m.Selectable() || m.HasCursor() {
		return false
	}
	// What is on screen comes first, so opening a file does not scroll.
	top := m.vp.YOffset()
	bottom := min(m.nrows(), top+m.vpHeight())
	for row := top; row < bottom; row++ {
		if m.itemAt(row).Kind == ItemLine {
			m.cursor = row
			return true
		}
	}
	for row := top; row < bottom; row++ {
		if m.itemAt(row).Kind == ItemHunk {
			m.cursor = row
			return true
		}
	}
	// Nothing visible: the changes are below the fold, so go to the next one.
	for row := bottom; row < m.nrows(); row++ {
		if m.itemAt(row).Kind == ItemLine {
			return m.setCursor(row)
		}
	}
	return false
}

// RestoreCursor puts the cursor back on the line it was on, or on the nearest
// change when that line is gone. It never falls back to the file header, and does
// nothing when the cursor is already placed.
func (m *Model) RestoreCursor(a CursorAnchor) {
	if !m.Selectable() || m.HasCursor() {
		return
	}
	// Put the top of the pane back first: a diff that lost rows above it would
	// otherwise slide everything the reader was looking at down the screen.
	if a.Top > 0 {
		m.JumpTo(m.rowForDiffLine(a.Top))
	}
	// A document holds several files, so the line alone does not name the place.
	inFile := func(row int) bool {
		if a.Path == "" {
			return true
		}
		f := m.rowFile(row)
		return f != nil && f.Path() == a.Path
	}
	if a.HasLine {
		for row := 0; row < m.nrows(); row++ {
			if !inFile(row) {
				continue
			}
			for _, l := range m.linesAt(row) {
				if key, ok := stage.LineOf(l); ok && key == a.Line {
					m.setCursor(row)
					return
				}
			}
		}
	}
	// The line is gone: continue from the next change, then the one before it.
	for row := max(0, a.Row); row < m.nrows(); row++ {
		if k := m.itemAt(row).Kind; inFile(row) && (k == ItemLine || k == ItemHunk) {
			m.setCursor(row)
			return
		}
	}
	for row := min(a.Row, m.nrows()) - 1; row >= 0; row-- {
		if k := m.itemAt(row).Kind; inFile(row) && (k == ItemLine || k == ItemHunk) {
			m.setCursor(row)
			return
		}
	}
}

// CursorToBand puts the cursor on the first thing that acts inside block i, so the
// sidebar and the pane name the same file.
func (m *Model) CursorToBand(i int) bool {
	if !m.Selectable() || !m.HasBand(i) {
		return false
	}
	start := m.fileBands[i]
	end := m.nrows()
	if i+1 < len(m.fileBands) {
		end = m.fileBands[i+1]
	}
	for row := max(0, start); row < min(end, m.nrows()); row++ {
		if k := m.itemAt(row).Kind; k == ItemLine || k == ItemHunk {
			return m.setCursor(row)
		}
	}
	return false
}

// CursorTo places the cursor on a row directly, which is what a click does.
func (m *Model) CursorTo(row int) bool {
	if !m.Selectable() {
		return false
	}
	if m.itemAt(row).Kind == ItemNone {
		return false
	}
	return m.setCursor(row)
}

// setCursor places the cursor and keeps it in view; the same row is not a move.
func (m *Model) setCursor(row int) bool {
	if row < 0 || row >= m.nrows() {
		return false
	}
	if row == m.cursor {
		return false
	}
	m.cursor = row
	m.CancelScroll()
	m.scrollCursorIntoView()
	return true
}

// scrollCursorIntoView glides the window only as far as the cursor needs. Wrapped
// rows are several display lines tall, so this asks the mapping that paints the bar
// rather than comparing row numbers.
func (m *Model) scrollCursorIntoView() {
	if m.cursor < 0 {
		return
	}
	h := m.vpHeight()
	y := m.vp.YOffset()
	line := m.displayLineOf(m.cursor)
	switch {
	case line < y:
		m.vp.SetYOffset(line)
	case line >= y+h:
		m.vp.SetYOffset(line - h + 1)
	}
}

// Selection is the set of changed lines the cursor names: the file on a band, the
// hunk on a header, the line on a change, every change in a range. A replacement is
// one decision, so a line brings its pair.
func (m Model) Selection() *stage.Selection {
	sel := stage.NewSelection()
	if !m.Selectable() || !m.HasCursor() {
		return sel
	}
	file := m.rowFile(m.cursor)
	if file == nil {
		return sel
	}
	from, to := m.cursor, m.cursor
	if m.HasRange() {
		from, to = min(m.anchor, m.cursor), max(m.anchor, m.cursor)
	}
	if from == to {
		switch item := m.itemAt(m.cursor); item.Kind {
		case ItemFile:
			return stage.AllSelection(file)
		case ItemHunk:
			// A hunk is named by its rows: a document numbers them across files.
			start, end := m.hunkRows(item.Hunk)
			for row := start; row <= end; row++ {
				if m.rowFile(row) != file {
					continue
				}
				for _, l := range m.linesAt(row) {
					if key, ok := stage.LineOf(l); ok {
						sel.Add(key)
					}
				}
			}
			return stage.Pair(file, sel)
		}
	}
	for row := from; row <= to; row++ {
		// A range stays in the file it started in: a row past a band is another file.
		if m.rowFile(row) != file {
			continue
		}
		for _, l := range m.linesAt(row) {
			if key, ok := stage.LineOf(l); ok {
				sel.Add(key)
			}
		}
	}
	return stage.Pair(file, sel)
}

// SelectionScope is the noun for what the cursor covers, for the footer.
func (m Model) SelectionScope() string {
	if m.HasRange() {
		return "range"
	}
	switch m.CursorItem().Kind {
	case ItemFile:
		return "file"
	case ItemHunk:
		return "hunk"
	case ItemLine:
		return "line"
	}
	return ""
}

// Pending is the set of lines marked in select mode.
func (m Model) Pending() *stage.Selection {
	if m.pending == nil {
		return stage.NewSelection()
	}
	return m.pending
}

// ToggleMark flips the pending state of what the cursor names, as one decision.
func (m *Model) ToggleMark() {
	sel := m.Selection()
	if sel.Empty() {
		return
	}
	path := ""
	if f := m.rowFile(m.cursor); f != nil {
		path = f.Path()
	}
	if m.pending == nil || m.markPath != path {
		// Marks belong to one file at a time: a selection names lines, not files.
		m.pending = stage.NewSelection()
		m.markPath = path
	}
	marked := false
	for _, l := range sel.Lines() {
		if m.pending.Has(l) {
			marked = true
			break
		}
	}
	if marked {
		m.pending.Difference(sel)
		return
	}
	m.pending.Merge(sel)
}

// ClearPending drops every mark.
func (m *Model) ClearPending() {
	m.pending = stage.NewSelection()
	m.markPath = ""
}

// rowPending reports whether a row holds a marked line.
func (m Model) rowPending(row int) bool {
	if m.pending == nil || m.pending.Empty() {
		return false
	}
	for _, l := range m.linesAt(row) {
		key, ok := stage.LineOf(l)
		if ok && m.pending.Has(key) {
			return true
		}
	}
	return false
}

// barAt renders the two reserved gutter columns for one row, never the line itself,
// so the wash and the syntax colours stay as they were.
func (m Model) barAt(row int) string {
	if m.barWidth() == 0 {
		return ""
	}
	bar := " "
	switch {
	case m.cursor == row && m.cursorOn:
		bar = m.styles.CursorBar().Render("▌")
	case m.cursor == row:
		bar = m.styles.HunkEdge().Render("▏")
	}

	mark := " "
	switch {
	case m.rowPending(row):
		mark = m.styles.PendingMark().Render("│")
	case m.inRange(row):
		mark = m.styles.StageMark().Render("║")
	case m.inHunkBody(row):
		mark = m.styles.HunkEdge().Render("│")
	default:
		if h, ok := m.hunkAtRow(row); ok {
			_ = h
			mark = m.styles.HunkEdge().Render("┌")
		}
	}
	return bar + mark
}

// inRange reports whether a row lies between the two ends of a range, in the
// file the range belongs to.
func (m Model) inRange(row int) bool {
	if !m.HasRange() {
		return false
	}
	if m.rowFile(row) != m.rowFile(m.cursor) {
		return false
	}
	return row >= min(m.anchor, m.cursor) && row <= max(m.anchor, m.cursor)
}

// inHunkBody reports whether a row sits inside a hunk but is not its header.
func (m Model) inHunkBody(row int) bool {
	i := sort.SearchInts(m.hunks, row)
	if i == 0 || i > len(m.hunks) {
		return false
	}
	start, end := m.hunkRows(i - 1)
	return row > start && row <= end
}
