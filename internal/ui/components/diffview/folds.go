package diffview

import "strconv"

const (
	foldClosedMarker = "\uf061"
	foldOpenMarker   = "\uf063"
)

type foldSection struct {
	key   string
	start int
	end   int
}

func (m Model) logicalRows() int {
	if m.doc != nil {
		return m.doc.count()
	}
	return len(m.rows)
}

func (m Model) logicalAt(i int) int {
	if i < 0 || i >= len(m.visibleRows) {
		return i
	}
	return m.visibleRows[i]
}

func (m Model) displayOfLogical(i int) int {
	if i < 0 || i >= len(m.visibleRows) {
		return i
	}
	for display, logical := range m.visibleRows {
		if logical == i {
			return display
		}
	}
	return -1
}

func (m Model) foldSections() []foldSection {
	sections := make([]foldSection, 0, len(m.hunks)+len(m.fileBands)+1)
	if n := len(m.noteRows); n > 1 {
		sections = append(sections, foldSection{key: "notes", start: 0, end: n - 1})
	}
	for i, start := range m.hunks {
		_, end := m.logicalHunkRows(i)
		sections = append(sections, foldSection{key: "hunk:" + strconv.Itoa(i), start: start, end: end})
	}
	for i, start := range m.fileBands {
		end := m.logicalRows() - 1
		if i+1 < len(m.fileBands) {
			end = m.fileBands[i+1] - 1
		}
		if end > start {
			sections = append(sections, foldSection{key: "file:" + strconv.Itoa(i), start: start, end: end})
		}
	}
	return sections
}

func (m Model) logicalHunkRows(h int) (int, int) {
	if h < 0 || h >= len(m.hunks) {
		return 0, 0
	}
	start := m.hunks[h]
	end := m.logicalRows() - 1
	if h+1 < len(m.hunks) {
		end = m.hunks[h+1] - 1
	}
	return start, end
}

func (m Model) foldKey(logical int) (string, bool) {
	if logical < 0 || logical >= m.logicalRows() {
		return "", false
	}
	if logical == 0 && len(m.noteRows) > 1 {
		return "notes", true
	}
	if i := indexOfInt(m.fileBands, logical); i >= 0 {
		return "file:" + strconv.Itoa(i), true
	}
	if h := m.logicalHunkAtRow(logical); h >= 0 {
		return "hunk:" + strconv.Itoa(h), true
	}
	return "", false
}

// foldTargetKey resolves the section a cursor row acts on: a hunk body can
// still toggle the hunk that contains it, but it is not itself a fold header.
func (m Model) foldTargetKey(logical int) (string, bool) {
	if logical >= 0 && logical < len(m.noteRows) && len(m.noteRows) > 1 {
		return "notes", true
	}
	if key, ok := m.foldKey(logical); ok {
		return key, true
	}
	if h := m.logicalHunkOfRow(logical); h >= 0 {
		return "hunk:" + strconv.Itoa(h), true
	}
	return "", false
}

func (m Model) logicalHunkAtRow(row int) int {
	i := sortInts(m.hunks, row)
	if i < len(m.hunks) && m.hunks[i] == row {
		return i
	}
	return -1
}

func (m Model) logicalHunkOfRow(row int) int {
	return sortInts(m.hunks, row) - 1
}

func (m *Model) recomputeFoldWidth() {
	if m.doc != nil {
		m.contentWidth = m.doc.width(m.vpWidth(), m.split, m.showLineNumbers)
		return
	}
	m.contentWidth = 0
	for i := range m.visibleRows {
		m.contentWidth = max(m.contentWidth, m.rowWidth(i, m.rowAt(i)))
	}
}

func (m *Model) resetFolds() {
	m.folds = nil
	m.visibleRows = nil
	m.gut.visible = nil
	m.cache.reset()
}

func (m Model) foldableAt(display int) bool {
	_, ok := m.foldKey(m.logicalAt(display))
	return ok
}

func (m Model) foldMarker(display int) string {
	if !m.foldableAt(display) {
		return ""
	}
	base := m.styles.DiffContext()
	if r := m.rowAt(display); !r.pre {
		base = m.lineStyles(r.line)
	}
	if m.foldMarkerState(display) {
		return base.Render(foldOpenMarker + " ")
	}
	return base.Render(foldClosedMarker + " ")
}

func (m Model) foldMarkerState(display int) bool {
	key, ok := m.foldKey(m.logicalAt(display))
	return ok && m.folds[key]
}

func (m Model) foldMarkerWidth(display int) int {
	if m.foldableAt(display) {
		return 2
	}
	return 0
}

func (m *Model) rebuildFolds() {
	logical := m.logicalRows()
	hidden := make([]bool, logical)
	for _, section := range m.foldSections() {
		if !m.folds[section.key] {
			continue
		}
		for i := section.start + 1; i <= section.end && i < logical; i++ {
			hidden[i] = true
		}
	}
	rows := make([]int, 0, logical)
	for i, hide := range hidden {
		if !hide {
			rows = append(rows, i)
		}
	}
	m.visibleRows = rows
	m.gut.visible = rows
	m.cache.reset()
}

func (m *Model) ToggleFold() {
	row := m.cursor
	if row < 0 || row >= m.nrows() {
		row = m.vp.YOffset()
	}
	logical := m.logicalAt(row)
	key, ok := m.foldTargetKey(logical)
	if !ok {
		return
	}
	wasFolded := m.folds[key]
	if m.folds == nil {
		m.folds = make(map[string]bool)
	}
	if wasFolded {
		delete(m.folds, key)
	} else {
		m.folds[key] = true
	}
	before := m.folds[key]
	m.rebuildFolds()
	m.recomputeFoldWidth()
	m.pushContent()
	if before {
		if anchor := m.displayOfLogical(logical); anchor >= 0 {
			m.vp.SetYOffset(max(0, min(anchor, m.maxScroll())))
		}
		if m.HasCursor() && !m.visible(logical) {
			if anchor := m.displayOfLogical(logical); anchor >= 0 {
				m.cursor = anchor
			}
		}
		return
	}
	anchor := m.displayOfLogical(logical)
	if anchor >= 0 {
		m.vp.SetYOffset(max(0, min(anchor, m.maxScroll())))
	}
}

func (m Model) visible(logical int) bool {
	for _, row := range m.visibleRows {
		if row == logical {
			return true
		}
	}
	return false
}

func indexOfInt(values []int, want int) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}

func sortInts(values []int, want int) int {
	lo, hi := 0, len(values)
	for lo < hi {
		mid := (lo + hi) / 2
		if values[mid] < want {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
