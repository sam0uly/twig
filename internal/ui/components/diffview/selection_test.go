package diffview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/ui/styles"
)

// selectable is a sized model with the staging cursor turned on.
func selectable() Model {
	m := newSized(false, 60)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	return m
}

// rowOfKind finds the first row holding a change of a kind. The row order
// within a change group is the renderer's business, so a test must not assume
// which of a deletion and an addition comes first.
func rowOfKind(m Model, k diff.LineKind) int {
	for i := 0; i < m.nrows(); i++ {
		for _, l := range m.linesAt(i) {
			if l.Kind == k {
				return i
			}
		}
	}
	return -1
}

// lastRowOfKind finds the final row holding a change of a kind.
func lastRowOfKind(m Model, k diff.LineKind) int {
	last := -1
	for i := 0; i < m.nrows(); i++ {
		for _, l := range m.linesAt(i) {
			if l.Kind == k {
				last = i
			}
		}
	}
	return last
}

func TestCursorSkipsRowsNothingActsOn(t *testing.T) {
	m := selectable()

	// The first press places the cursor on the nearest actionable row at the
	// top of the view: the file header.
	if !m.MoveCursor(1) {
		t.Fatal("first move should place the cursor")
	}
	if got := m.CursorRow(); got != 0 {
		t.Fatalf("cursor on row %d, want 0", got)
	}
	if k := m.CursorItem().Kind; k != ItemFile {
		t.Fatalf("item kind %v, want ItemFile", k)
	}

	// The next press lands on the hunk header, not on the context line under
	// it: a dead row would make the key look broken.
	if !m.MoveCursor(1) {
		t.Fatal("second move should move")
	}
	if got := m.CursorRow(); got != 1 {
		t.Fatalf("cursor on row %d, want the hunk header at 1", got)
	}
	if k := m.CursorItem().Kind; k != ItemHunk {
		t.Fatalf("item kind %v, want ItemHunk", k)
	}

	// A third press skips the context rows and lands on a change line.
	if !m.MoveCursor(1) {
		t.Fatal("third move should move")
	}
	if k := m.CursorItem().Kind; k != ItemLine {
		t.Fatalf("item kind %v, want ItemLine", k)
	}
	first := min(rowOfKind(m, diff.Deletion), rowOfKind(m, diff.Addition))
	if got := m.CursorRow(); got != first {
		t.Fatalf("cursor on row %d, want the first change at %d", got, first)
	}

	// The cursor stops at the last change rather than wrapping or landing on
	// the trailing context.
	last := max(lastRowOfKind(m, diff.Addition), lastRowOfKind(m, diff.Deletion))
	if !m.MoveCursor(100) {
		t.Fatal("a long move should still move")
	}
	if got := m.CursorRow(); got != last {
		t.Fatalf("cursor on row %d, want the last change at %d", got, last)
	}
	if m.MoveCursor(1) {
		t.Fatal("moving past the last change should be a no-op")
	}
}

func TestSelectionScopes(t *testing.T) {
	m := selectable()

	if !m.CursorTo(0) {
		t.Fatal("cursor to the file header")
	}
	if got := m.Selection(); got.Len() != 3 {
		t.Fatalf("file scope selected %d lines, want 3", got.Len())
	}
	if got := m.SelectionScope(); got != "file" {
		t.Fatalf("scope %q, want file", got)
	}

	if !m.CursorTo(1) {
		t.Fatal("cursor to the hunk header")
	}
	sel := m.Selection()
	if got := m.SelectionScope(); got != "hunk" {
		t.Fatalf("scope %q, want hunk", got)
	}
	if !sel.Has(stage.NewPre(4)) || !sel.Has(stage.NewPost(4)) || !sel.Has(stage.NewPost(5)) {
		t.Fatalf("hunk selection is missing a line: %v", sel.Lines())
	}
	if sel.Len() != 3 {
		t.Fatalf("hunk selected %d lines, want 3", sel.Len())
	}

	del := rowOfKind(m, diff.Deletion)
	if !m.CursorTo(del) {
		t.Fatal("cursor to the deletion")
	}
	sel = m.Selection()
	if got := m.SelectionScope(); got != "line" {
		t.Fatalf("scope %q, want line", got)
	}
	// A replacement is one decision, so the line under the cursor brings the
	// other half of the pair with it.
	if sel.Len() != 2 || !sel.Has(stage.NewPre(4)) || !sel.Has(stage.NewPost(4)) {
		t.Fatalf("line scope selected %v, want the replacement pair", sel.Lines())
	}

	// A pure addition has no pair and stands alone.
	last := lastRowOfKind(m, diff.Addition)
	if !m.CursorTo(1) {
		t.Fatal("cursor to the hunk header")
	}
	for m.CursorRow() != last {
		if !m.MoveCursor(1) {
			t.Fatalf("could not reach the last addition at %d", last)
		}
	}
	if sel := m.Selection(); sel.Len() != 1 || !sel.Has(stage.NewPost(5)) {
		t.Fatalf("line scope selected %v, want just the lone addition", sel.Lines())
	}
}

func TestCursorDoesNotReflowTheDiff(t *testing.T) {
	off := newSized(false, 60)
	on := selectable()

	for _, m := range []Model{off, on} {
		lines := strings.Split(m.View(), "\n")
		if len(lines) != 12 {
			t.Fatalf("%d lines, want 12", len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != 60 {
				t.Fatalf("line %d width %d, want 60", i, w)
			}
		}
	}

	// The gutter is reserved, not inserted: every row keeps its content and
	// its width, losing two cells of trailing padding to the bar.
	trim := func(s string) string {
		return strings.TrimRight(strings.TrimSuffix(s, "│"), " ")
	}
	want := strings.Split(ansi.Strip(off.View()), "\n")
	got := strings.Split(ansi.Strip(on.View()), "\n")
	for i := range want {
		// The bar is two cells, not two bytes: the mark column holds a
		// box-drawing glyph.
		runes := []rune(got[i])
		if len(runes) < 2 {
			t.Fatalf("row %d is too short to carry the bar: %q", i, got[i])
		}
		if trim(string(runes[2:])) != trim(want[i]) {
			t.Fatalf("row %d moved its content:\ngot  %q\nwant %q", i, string(runes[2:]), want[i])
		}
	}
}

// TestCursorDoesNotBurnTheRowCache makes sure moving the cursor does not throw
// away rendered rows: the bar is a prefix, so the cached text stays valid.
func TestCursorDoesNotBurnTheRowCache(t *testing.T) {
	m := selectable()
	add := rowOfKind(m, diff.Addition)
	if !m.CursorTo(add) {
		t.Fatal("place the cursor")
	}
	before := m.renderRow(add, m.rowAt(add))
	if !m.MoveCursor(1) {
		t.Fatal("move the cursor")
	}
	if after := m.renderRow(add, m.rowAt(add)); after != before {
		t.Fatal("a row was re-rendered when the cursor left it")
	}
}

func TestMarksToggleAsOneDecision(t *testing.T) {
	m := selectable()
	m.SetSelectMode(true)

	del := rowOfKind(m, diff.Deletion)
	if !m.CursorTo(del) {
		t.Fatal("place the cursor")
	}
	m.ToggleMark()
	if got := m.Pending().Len(); got != 2 {
		t.Fatalf("%d marked lines, want the pair", got)
	}
	m.ToggleMark()
	if got := m.Pending().Len(); got != 0 {
		t.Fatalf("%d marked lines after unmarking, want 0", got)
	}

	// A hunk marks whole, and unmarks whole.
	if !m.CursorTo(1) {
		t.Fatal("cursor to the hunk")
	}
	m.ToggleMark()
	if got := m.Pending().Len(); got != 3 {
		t.Fatalf("%d marked lines for a hunk, want 3", got)
	}
	m.ToggleMark()
	if got := m.Pending().Len(); got != 0 {
		t.Fatalf("%d marked lines after unmarking the hunk, want 0", got)
	}
}

// TestRestoreCursorFallsBackToTheNextChange: when the line the cursor named has
// just been staged away, the cursor continues at the next change rather than
// jumping to the file header, where one keystroke would act on the whole file.
func TestRestoreCursorFallsBackToTheNextChange(t *testing.T) {
	m := selectable()

	// The pane is replaced by the remaining change: one line, further down.
	m.SetFile(diff.Generate("main.go", diff.Modified,
		"one\ntwo\nthree\nfour\n", "one\ntwo\nthree\nFOUR\n", diff.GenerateOptions{Context: 3}))
	if m.HasCursor() {
		t.Fatal("the cursor should be dropped by a new document")
	}

	anchor := CursorAnchor{Path: "main.go", HasLine: true, Line: stage.NewPre(4), Row: 3}
	m.RestoreCursor(anchor)

	if !m.HasCursor() {
		t.Fatal("the cursor was not restored")
	}
	if k := m.CursorItem().Kind; k != ItemLine {
		t.Fatalf("cursor landed on %v, want a change line", k)
	}
	if m.CursorRow() == 0 {
		t.Fatal("the cursor fell back to the file header")
	}
}

// TestRestoreCursorLeavesNoCursorWhenNothingIsLeft: a file with no changes
// left must not hand the cursor to a row that stages everything.
func TestRestoreCursorLeavesNoCursorWhenNothingIsLeft(t *testing.T) {
	m := selectable()
	m.SetFile(diff.Generate("main.go", diff.Modified, "same\n", "same\n", diff.GenerateOptions{Context: 3}))
	m.RestoreCursor(CursorAnchor{Path: "main.go", HasLine: true, Line: stage.NewPre(4), Row: 2})
	if m.HasCursor() {
		t.Fatal("a diff with no changes must leave the cursor unplaced")
	}
}

// TestSplitViewWithCursorKeepsItsWidth is the regression test for the bar
// being added on top of rows that already filled the pane: in split mode each
// row used to come out two cells too wide, which pushed the borders apart.
func TestSplitViewWithCursorKeepsItsWidth(t *testing.T) {
	off := newSized(true, 60)
	on := newSized(true, 60)
	on.SetSelectable(true)
	on.SetCursorFocus(true)

	base := strings.Split(ansi.Strip(off.View()), "\n")
	got := strings.Split(ansi.Strip(on.View()), "\n")
	if len(base) != len(got) {
		t.Fatalf("%d rows with the bar, %d without", len(got), len(base))
	}
	for i := range base {
		if w := lipgloss.Width(got[i]); w != 60 {
			t.Fatalf("row %d is %d cells wide, want 60:\n%s", i, w, got[i])
		}
		runes := []rune(got[i])
		if len(runes) < 2 {
			t.Fatalf("row %d cannot carry the bar", i)
		}
		// Nothing on the row may be lost. Padding and the split filler do change,
		// because two cells less width divides into different halves, so the
		// comparison keeps only what was written there.
		content := func(s string) string {
			return strings.Map(func(r rune) rune {
				switch r {
				case ' ', '│', '╱':
					return -1
				}
				return r
			}, s)
		}
		if content(string(runes[2:])) != content(base[i]) {
			t.Fatalf("row %d lost content with the bar:\nwith %q\nwithout %q", i, string(runes[2:]), base[i])
		}
	}
}

// TestArrowsScrollWhenTheCursorCannotMove: at the last change in a diff taller
// than the pane, down must still move the view. A key that does nothing because
// the cursor ran out of rows reads as a broken key, not as an edge.
func TestArrowsScrollWhenTheCursorCannotMove(t *testing.T) {
	// Many changes, so the document is taller than the pane: only a tall diff
	// can scroll once the cursor has run out of rows.
	var pre, post strings.Builder
	for i := 0; i < 40; i++ {
		pre.WriteString("line\n")
		if i%4 == 0 {
			post.WriteString("CHANGED\n")
			continue
		}
		post.WriteString("line\n")
	}

	m := New(false, styles.New())
	m.SetSize(40, 8)
	m.SetFile(diff.Generate("a.txt", diff.Modified, pre.String(), post.String(), diff.GenerateOptions{Context: 1}))
	m.SetSelectable(true)

	// The first press places the cursor, the second walks it to the last item.
	m.MoveCursor(1)
	m.MoveCursor(1000)
	last := m.CursorRow()
	y := m.YOffset()

	if !m.MoveCursor(1) {
		t.Fatal("down went dead at the last change")
	}
	if m.YOffset() == y {
		t.Fatal("down did not scroll the pane")
	}
	if m.CursorRow() != last {
		t.Fatalf("the cursor left the last change (row %d, want %d)", m.CursorRow(), last)
	}

	// At the top there is nothing above to scroll to, so up stays put rather
	// than running off the end.
	top := New(false, styles.New())
	top.SetSize(40, 8)
	top.SetFile(diff.Generate("a.txt", diff.Modified, pre.String(), post.String(), diff.GenerateOptions{Context: 1}))
	top.SetSelectable(true)
	top.MoveCursor(1) // places the cursor on the first item
	if top.CursorRow() != 0 {
		t.Fatalf("cursor on row %d, want the first item", top.CursorRow())
	}
	if top.MoveCursor(-1) {
		t.Fatal("up moved at the very top")
	}
	if top.CursorRow() != 0 || top.YOffset() != 0 {
		t.Fatalf("up drifted to row %d, offset %d", top.CursorRow(), top.YOffset())
	}
}

// TestDocumentCursorKnowsItsFile: the all-changes view is one document over
// many files, so a keystroke there has to be able to name the file it acts on.
func TestDocumentCursorKnowsItsFile(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.go", 8, "CHANGED A"),
		docTestFile("b.go", 8, "CHANGED B"),
	}
	var loads int
	m := newDocView(t, files, &loads)
	m.SetSelectable(true)

	if !m.PlaceCursor() {
		t.Fatal("the cursor was not placed in a document")
	}
	if got := m.CursorItem().Kind; got == ItemNone {
		t.Fatal("the cursor is on a row nothing acts on")
	}
	path, ok := m.CursorFile()
	if !ok || path != "a.go" {
		t.Fatalf("cursor file = %q, %v, want a.go", path, ok)
	}
	if sel := m.Selection(); sel.Empty() {
		t.Fatal("selecting the cursor's row produced nothing")
	}

	// Walking to the next file's change changes which file is named.
	for i := 0; i < 40; i++ {
		if !m.MoveCursor(1) {
			break
		}
		if path, ok := m.CursorFile(); ok && path == "b.go" {
			return
		}
	}
	t.Fatal("the cursor never reached the second file")
}
