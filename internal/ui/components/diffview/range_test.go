package diffview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/ui/styles"
)

// twoHunks is a diff with two change sites far enough apart to be separate
// hunks, so a range can cover one, the other, or both.
func twoHunks() *diff.File {
	var before, after strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&before, "line %d\n", i)
		if i == 8 || i == 30 {
			fmt.Fprintf(&after, "CHANGED %d\n", i)
			continue
		}
		fmt.Fprintf(&after, "line %d\n", i)
	}
	return diff.Generate("a.txt", diff.Modified, before.String(), after.String(), diff.GenerateOptions{Context: 3})
}

// longFile is a diff tall enough to scroll, with changes spread down it. The
// change at skip is left alone when skip is non-zero, which is how a test takes
// away the line the cursor was on.
func longFile(skip ...int) *diff.File {
	gone := 0
	if len(skip) > 0 {
		gone = skip[0]
	}
	var before, after strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&before, "line %d\n", i)
		if i%20 == 10 && i != gone {
			fmt.Fprintf(&after, "CHANGED %d\n", i)
			continue
		}
		fmt.Fprintf(&after, "line %d\n", i)
	}
	return diff.Generate("a.txt", diff.Modified, before.String(), after.String(), diff.GenerateOptions{Context: 3})
}

// twoHunkView is a sized pane holding twoHunks with the cursor available.
func twoHunkView() Model {
	m := New(false, styles.New())
	m.SetSize(60, 40)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetFile(twoHunks())
	return m
}

// hunkHeaderRow returns the n-th hunk header row, or -1.
func hunkHeaderRow(m Model, n int) int {
	seen := 0
	for row := 0; row < m.nrows(); row++ {
		if k := m.itemAt(row).Kind; k == ItemHunk {
			if seen == n {
				return row
			}
			seen++
		}
	}
	return -1
}

// TestRangeCoversEveryChangeBetweenItsEnds: V anchors where the cursor is, and
// moving the cursor grows the range, so the reader selects a span of changes
// without a second set of keys.
func TestRangeCoversEveryChangeBetweenItsEnds(t *testing.T) {
	m := twoHunkView()
	first, second := hunkHeaderRow(m, 0), hunkHeaderRow(m, 1)
	if first < 0 || second < 0 {
		t.Fatalf("fixture has hunks at %d and %d, want two", first, second)
	}

	if !m.CursorTo(first) {
		t.Fatal("cursor to the first hunk")
	}
	if !m.StartRange() {
		t.Fatal("V did not start a range")
	}
	if m.HasRange() {
		t.Fatal("a range that has not moved yet should cover nothing extra")
	}
	if !m.ExtendCursor(second - first) {
		t.Fatal("the range did not extend to the second hunk")
	}
	if !m.HasRange() {
		t.Fatal("the range did not become live")
	}
	if got := m.SelectionScope(); got != "range" {
		t.Fatalf("scope %q, want range", got)
	}

	// Both changes are replacements, so the range brings both pairs: two
	// deletions and two additions, and nothing from the context between them.
	sel := m.Selection()
	want := []stage.Line{stage.NewPre(8), stage.NewPost(8), stage.NewPre(30), stage.NewPost(30)}
	for _, l := range want {
		if !sel.Has(l) {
			t.Fatalf("range is missing %v: %v", l, sel.Lines())
		}
	}
	if sel.Len() != len(want) {
		t.Fatalf("range selected %d lines, want %d: %v", sel.Len(), len(want), sel.Lines())
	}
}

// TestMovingDropsTheRangeButExtendingKeepsIt separates the two cursor verbs:
// plain movement settles on one thing again, extending grows the span.
func TestMovingDropsTheRangeButExtendingKeepsIt(t *testing.T) {
	m := twoHunkView()
	first := hunkHeaderRow(m, 0)
	if !m.CursorTo(first) || !m.StartRange() {
		t.Fatal("could not start a range")
	}
	if !m.ExtendCursor(1) {
		t.Fatal("extend did not move")
	}
	if !m.HasRange() {
		t.Fatal("extending dropped the range")
	}
	if !m.MoveCursor(1) {
		t.Fatal("move did not move")
	}
	if m.RangeActive() || m.HasRange() {
		t.Fatal("plain movement kept the range, so the next stage would act on a span")
	}
	if got := m.SelectionScope(); got == "range" {
		t.Fatalf("scope is still %q after moving", got)
	}
}

// TestRangeIsDrawnInTheGutter: the span has to be visible, or the reader cannot
// check what the next keystroke will act on.
func TestRangeIsDrawnInTheGutter(t *testing.T) {
	m := twoHunkView()
	first, second := hunkHeaderRow(m, 0), hunkHeaderRow(m, 1)
	if !m.CursorTo(first) || !m.StartRange() {
		t.Fatal("could not start a range")
	}
	if !m.ExtendCursor(second - first) {
		t.Fatal("the range did not extend")
	}
	for _, row := range []int{first + 1, second - 1} {
		if got := m.barAt(row); !strings.Contains(got, "║") {
			t.Fatalf("row %d inside the range is not marked: %q", row, got)
		}
	}
	if got := m.barAt(first + 1); strings.Contains(got, "│") && !strings.Contains(got, "║") {
		t.Fatalf("the range mark is the hunk mark: %q", got)
	}
}

// TestHunkScopeIsTheHunksOwnLines: in a document the hunk indices are numbered
// across every file, so a hunk has to be resolved through its own rows.
func TestHunkScopeIsTheHunksOwnLines(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.go", 20, "CHANGED A"),
		docTestFile("b.go", 20, "CHANGED B"),
	}
	loads := 0
	m := newDocView(t, files, &loads)
	m.SetSelectable(true)

	row := -1
	for i := 0; i < m.nrows(); i++ {
		if f := m.rowFile(i); f != nil && f.Path() == "b.go" && m.itemAt(i).Kind == ItemHunk {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("the second file has no hunk header row")
	}
	if !m.CursorTo(row) {
		t.Fatal("cursor to the second file's hunk")
	}
	sel := m.Selection()
	if sel.Len() != 2 {
		t.Fatalf("hunk scope selected %d lines, want the second file's pair: %v", sel.Len(), sel.Lines())
	}
	if !sel.Has(stage.NewPre(11)) || !sel.Has(stage.NewPost(11)) {
		t.Fatalf("hunk scope selected %v, want line 11 of b.go", sel.Lines())
	}
}

// TestBlocksOfAFileAreNotFileHeaders: a document lays one file out in many
// blocks, and treating a block boundary as a file header made a keystroke in
// the middle of a large file act on the whole file.
func TestBlocksOfAFileAreNotFileHeaders(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 3*docBlockLines; i++ {
		fmt.Fprintf(&before, "line %d\n", i)
		if i%900 == 0 {
			fmt.Fprintf(&after, "changed %d\n", i)
			continue
		}
		fmt.Fprintf(&after, "line %d\n", i)
	}
	m := New(false, styles.New())
	m.SetSize(60, 20)
	m.SetSelectable(true)
	m.SetLines(diff.Generate("big.txt", diff.Modified, before.String(), after.String(), diff.GenerateOptions{Context: 3}))

	if got := len(m.fileBands); got != 1 {
		t.Fatalf("%d bands for a single-file document, want 1", got)
	}
	// The one band names the file, and no block boundary does.
	for row := 0; row < m.nrows(); row++ {
		if m.itemAt(row).Kind == ItemFile && !m.isBandRow(m.fileBands[0]) {
			t.Fatalf("row %d is treated as a file header inside one file", row)
		}
		if row == m.fileBands[0] {
			continue
		}
		if m.isBandRow(row) {
			t.Fatalf("row %d is a second band in a single-file document", row)
		}
	}
}

// TestRestoreCursorKeepsTheScreenStill: reloading a diff used to put the cursor
// back by scrolling it to the edge of the pane, which moved every row under the
// reader. The top of the pane is the reader's place, so that is what is restored.
func TestRestoreCursorKeepsTheScreenStill(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(60, 12)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetFile(longFile())

	// The first move only places the cursor; the distance is a move.
	if !m.MoveCursor(1) || !m.MoveCursor(30) {
		t.Fatal("the cursor did not move into the file")
	}
	row, offset := m.CursorRow(), m.YOffset()
	if offset == 0 {
		t.Fatalf("cursor on row %d with no scroll; the test needs a scrolled pane", row)
	}
	anchor := m.AnchorCursor("a.txt")
	topLine, ok := m.lineForRow(offset)
	if !ok {
		t.Fatalf("row %d has no line", offset)
	}

	// A reload replaces the document and starts it at the top.
	m.SetFile(longFile())
	if got := m.YOffset(); got != 0 {
		t.Fatalf("a reload should start at the top, got %d", got)
	}
	m.RestoreCursor(anchor)
	if got := m.CursorRow(); got != row {
		t.Fatalf("cursor on row %d after reload, want %d", got, row)
	}
	if got := m.YOffset(); got != offset {
		t.Fatalf("pane scrolled to %d after reload, want %d", got, offset)
	}
	if got, ok := m.lineForRow(m.YOffset()); !ok || got.Text != topLine.Text {
		t.Fatalf("the top row is now %q, want %q", got.Text, topLine.Text)
	}
}

// TestRestoreCursorWithoutAChangeKeepsTheScroll: when the change under the
// cursor is gone there is nothing to put it on, and the pane must still not jump
// to the top.
func TestRestoreCursorWithoutAChangeKeepsTheScroll(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(60, 12)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetFile(longFile())
	if !m.MoveCursor(1) || !m.MoveCursor(30) {
		t.Fatal("the cursor did not move into the file")
	}
	offset := m.YOffset()
	if offset == 0 {
		t.Fatal("the pane did not scroll, so the test proves nothing")
	}
	line, ok := stage.LineOf(m.CursorItem().Line)
	if !ok {
		t.Fatal("the cursor is not on a change")
	}
	anchor := m.AnchorCursor("a.txt")

	// The change under the cursor is staged, so that line is no longer in the
	// diff, but the rest of the file still is.
	m.SetFile(longFile(line.Num))
	m.RestoreCursor(anchor)
	if got := m.CursorRow(); got < 0 {
		t.Fatal("the cursor was not placed on the next change")
	}
	if got := m.YOffset(); got == 0 {
		t.Fatalf("the pane jumped to the top while the reader was %d rows in", offset)
	}
}

// TestRangeStopsAtTheFileItStartedIn: a selection names line numbers, not
// files, so a range that walked into the next block would stage rows from a
// file the reader never marked.
func TestRangeStopsAtTheFileItStartedIn(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.go", 20, "CHANGED A"),
		docTestFile("b.go", 20, "CHANGED B"),
	}
	loads := 0
	m := newDocView(t, files, &loads)
	m.SetSelectable(true)

	start := -1
	for row := 0; row < m.nrows(); row++ {
		if f := m.rowFile(row); f != nil && f.Path() == "a.go" && m.itemAt(row).Kind == ItemLine {
			start = row
			break
		}
	}
	if start < 0 {
		t.Fatal("the first file has no change row")
	}
	if !m.CursorTo(start) || !m.StartRange() {
		t.Fatal("could not start a range")
	}
	if !m.ExtendCursor(50) {
		t.Fatal("the range did not extend")
	}
	if path, _ := m.CursorFile(); path != "a.go" {
		t.Fatalf("the range walked into %q", path)
	}
	for row := 0; row < m.nrows(); row++ {
		if f := m.rowFile(row); f != nil && f.Path() == "b.go" && m.inRange(row) {
			t.Fatalf("row %d of the second file is drawn as part of the range", row)
		}
	}
	// Only the cursor file's replacement, not the second file's.
	if got := m.Selection().Len(); got != 2 {
		t.Fatalf("range selected %d lines, want just a.go's pair", got)
	}
}

// TestFollowEditInStaysInItsFile: in the all-changes document the same text can
// appear in several files, so following an edit has to land in the file that
// changed rather than in the first block that matches.
func TestFollowEditInStaysInItsFile(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.go", 20, "SAME TEXT"),
		docTestFile("b.go", 20, "SAME TEXT"),
	}
	loads := 0
	m := newDocView(t, files, &loads)
	m.SetSelectable(true)

	line := diff.Line{}
	for i := range files[1].Hunks {
		h, ok := files[1].HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			if l.Kind == diff.Addition {
				line = l
			}
		}
	}
	if line.Kind != diff.Addition {
		t.Fatal("the fixture has no addition")
	}
	if !m.FollowEditIn("b.go", line) {
		t.Fatal("the edit was not found in its own file")
	}
	path, ok := m.CursorFile()
	if !ok || path != "b.go" {
		t.Fatalf("cursor landed in %q, want b.go", path)
	}
}

// TestWrapModeShowsTheCursor: with soft wrap on the viewport paints the rows
// itself, so the bar has to be part of them, and the width it wraps at has to
// leave room for it.
func TestWrapModeShowsTheCursor(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(40, 12)
	m.SetWrap(true)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetFile(longFile())

	if !m.MoveCursor(1) || !m.MoveCursor(6) {
		t.Fatal("the cursor did not move")
	}
	if got := m.CursorRow(); got < 0 {
		t.Fatal("no cursor")
	}
	view := m.View()
	if !strings.Contains(view, "▌") {
		t.Fatalf("the cursor bar is not painted in wrap mode:\n%s", view)
	}
	// The bar must be on the display line the cursor names, not just somewhere
	// on the screen: wrapPrefix and the viewport have to agree on how tall a
	// wrapped row is. Scrolling is where the two scales are easiest to get
	// wrong, so it is checked after the keys that move the window too.
	checkBar := func(label string) {
		t.Helper()
		view := m.View()
		lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
		want := m.displayLineOf(m.CursorRow()) - m.YOffset()
		if want < 0 || want >= len(lines) || !strings.Contains(lines[want], "▌") {
			t.Fatalf("%s: the bar is not on display line %d, the one the cursor names:\n%s",
				label, want, view)
		}
	}
	m.SetSmooth(false)
	checkBar("before scrolling")
	for i := 0; i < 3; i++ {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next
		checkBar("after paging down")
	}
	view = m.View()
	for i, line := range strings.Split(strings.TrimSuffix(view, "\n"), "\n") {
		if w := lipgloss.Width(line); w != 40 {
			t.Fatalf("wrapped line %d is %d wide, want 40", i, w)
		}
	}
}

// TestFollowEditInBandStaysInItsBlock: the all-changes view names the block, so
// the search is that block's rows and the cursor lands in the file that changed
// even when an identical text sits in a block above it.
func TestFollowEditInBandStaysInItsBlock(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.go", 20, "SAME"),
		docTestFile("b.go", 20, "SAME"),
	}
	loads := 0
	m := newDocView(t, files, &loads)
	m.SetSelectable(true)

	var line diff.Line
	for i := range files[1].Hunks {
		h, ok := files[1].HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			if l.Kind == diff.Addition {
				line = l
			}
		}
	}
	if line.Kind != diff.Addition {
		t.Fatal("the fixture has no addition")
	}
	if !m.FollowEditInBand(1, "b.go", line) {
		t.Fatal("the edit was not found in its block")
	}
	if path, _ := m.CursorFile(); path != "b.go" {
		t.Fatalf("cursor landed in %q, want b.go", path)
	}
}
