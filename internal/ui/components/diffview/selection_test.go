package diffview

import (
	"strconv"
	"strings"
	"testing"
	"time"

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
	if got := m.CursorRow(); got != rowOfKind(m, diff.Deletion) {
		t.Fatalf("cursor on row %d, want the first change", got)
	}

	// The cursor stops at the last change rather than wrapping or landing on
	// the trailing context. A change group shows its deletions first, so the
	// last row of the group is the last addition.
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
	// its width, losing one cell of trailing padding to the bar.
	trim := func(s string) string {
		return strings.TrimRight(strings.TrimSuffix(s, "│"), " ")
	}
	want := strings.Split(ansi.Strip(off.View()), "\n")
	got := strings.Split(ansi.Strip(on.View()), "\n")
	for i := range want {
		runes := []rune(got[i])
		if len(runes) < 1 {
			t.Fatalf("row %d is too short to carry the bar: %q", i, got[i])
		}
		if trim(string(runes[1:])) != trim(want[i]) {
			t.Fatalf("row %d moved its content:\ngot  %q\nwant %q", i, string(runes[1:]), want[i])
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
		if len(runes) < 1 {
			t.Fatalf("row %d cannot carry the bar", i)
		}
		// Nothing on the row may be lost. Padding and the split filler do change,
		// because one cell less width divides into different halves, so the
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
		if content(string(runes[1:])) != content(base[i]) {
			t.Fatalf("row %d lost content with the bar:\nwith %q\nwithout %q", i, string(runes[1:]), base[i])
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

// tallModel is a document far longer than the pane, with a change every few
// lines so the cursor has somewhere to land all the way down. pad widens every
// line, which is how a wrap test gets rows taller than one display line.
func tallModel(t *testing.T, pad int) Model {
	t.Helper()
	fill := strings.Repeat("x", pad)
	var a, b strings.Builder
	for i := range 400 {
		a.WriteString("line " + strconv.Itoa(i) + " " + fill + "\n")
		if i%8 == 0 {
			b.WriteString("changed " + strconv.Itoa(i) + " " + fill + "\n")
			continue
		}
		b.WriteString("line " + strconv.Itoa(i) + " " + fill + "\n")
	}
	m := New(false, styles.New())
	m.SetSize(80, 12)
	m.SetSmooth(false)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetFile(diff.Generate("tall.go", diff.Modified, a.String(), b.String(),
		diff.GenerateOptions{Context: 2}))
	return m
}

// TestPagingMovesTheCursor: paging is moving the cursor a screenful, the way
// it moves a line with j and k. Paging the window on its own used to leave the
// cursor, and therefore the row a keystroke acts on, off the screen.
func TestPagingMovesTheCursor(t *testing.T) {
	m := tallModel(t, 0)
	if !m.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	for i := 0; i < 3; i++ {
		m.MoveCursor(1)
	}
	start := m.CursorRow()

	m.PageDown()
	if m.YOffset() == 0 {
		t.Fatal("pgdown did not move the window")
	}
	if !m.cursorInWindow() {
		t.Fatalf("cursor row %d is off the window at %d", m.CursorRow(), m.YOffset())
	}
	if got := m.CursorRow(); got <= start {
		t.Fatalf("pgdown left the cursor on row %d, it started on %d", got, start)
	}
	want := start
	for i := 0; i < m.vpHeight(); i++ {
		if next := m.nextItemRow(want, 1); next < 0 {
			break
		} else {
			want = next
		}
	}
	if got := m.CursorRow(); got != want {
		t.Fatalf("cursor on row %d after a page, want %d", got, want)
	}

	// And back up, onto the row a screenful above.
	m.PageUp()
	if got := m.CursorRow(); got >= want {
		t.Fatalf("pgup left the cursor on row %d, it was on %d", got, want)
	}
	if !m.cursorInWindow() {
		t.Fatalf("pgup left the cursor at %d outside the window at %d", m.CursorRow(), m.YOffset())
	}
}

// TestPagingInARangeExtendsIt: a range is extended by the same keys that move
// the cursor, and a page is a line times the height of the pane.
func TestPagingInARangeExtendsIt(t *testing.T) {
	m := tallModel(t, 0)
	if !m.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	for i := 0; i < 3; i++ {
		m.MoveCursor(1)
	}
	if !m.StartRange() {
		t.Fatal("no range to anchor")
	}
	head, sel := m.CursorRow(), m.Selection().Len()
	if sel == 0 {
		t.Fatal("the range is empty at its anchor")
	}

	m.PageDown()
	if got := m.CursorRow(); got <= head {
		t.Fatalf("paging left the range head on row %d, it was on %d", got, head)
	}
	if got := m.Selection().Len(); got <= sel {
		t.Fatalf("paging left the selection at %d lines, it was %d", got, sel)
	}
	if !m.cursorInWindow() {
		t.Fatalf("the head at %d is off the window at %d", m.CursorRow(), m.YOffset())
	}
}

// TestGotoTopAndBottomSitOnAChange: gg and G go to the ends of the diff, and
// the cursor goes with them. G on the last line of a long diff used to leave
// the cursor wherever it happened to be.
func TestGotoTopAndBottomSitOnAChange(t *testing.T) {
	m := tallModel(t, 0)
	if !m.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	for i := 0; i < 40; i++ {
		m.MoveCursor(1)
	}

	m.GotoTop()
	if !m.cursorInWindow() {
		t.Fatalf("the top left the cursor at %d outside the window at %d", m.CursorRow(), m.YOffset())
	}
	if got := m.CursorRow(); got != m.firstItemRow(0) {
		t.Fatalf("the top put the cursor on row %d, want the first row that acts at %d",
			got, m.firstItemRow(0))
	}
	if got := m.CursorItem().Kind; got == ItemNone {
		t.Fatal("the cursor is on a row nothing acts on")
	}

	m.GotoBottom()
	if !m.cursorInWindow() {
		t.Fatalf("the end left the cursor at %d outside the window at %d", m.CursorRow(), m.YOffset())
	}
	if got, want := m.CursorRow(), m.lastItemRow(); got != want {
		t.Fatalf("the end put the cursor on row %d, want the last row that acts at %d", got, want)
	}
	if got := m.CursorItem().Kind; got == ItemNone {
		t.Fatal("the cursor is on a row nothing acts on")
	}
}

// TestWrappedPagingKeepsTheCursor: wrapped rows are several display lines
// tall, so keeping the cursor in view has to work in display lines.
func TestWrappedPagingKeepsTheCursor(t *testing.T) {
	// Lines wide enough that every row wraps to two display lines at 80 cells.
	m := tallModel(t, 80)
	m.SetWrap(true)
	if !m.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	for i := 0; i < 3; i++ {
		m.MoveCursor(1)
	}
	for i := 0; i < 4; i++ {
		m.PageDown()
		if !m.cursorInWindow() {
			t.Fatalf("wrapped paging left the cursor at display line %d outside the window at %d",
				m.displayLineOf(m.CursorRow()), m.YOffset())
		}
		// wrapPrefix is only useful if it counts the rows the way the viewport
		// does: the bar is placed with it, and so is the cursor this rule holds
		// in view.
		if got, want := m.vp.TotalLineCount(), m.displayLineOf(m.nrows()); got != want {
			t.Fatalf("the viewport counts %d display lines, the wrap prefix %d", got, want)
		}
	}
}

// TestCursorMovesGlide: a key that moves the cursor can move the window too, and
// that move is a scroll like any other when smooth scrolling is on. The frame
// command it starts has to be handed to the runtime, or the glide never runs.
func TestCursorMovesGlide(t *testing.T) {
	m := tallModel(t, 0)
	m.SetSmooth(true)
	if !m.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	// A step that stays inside the window scrolls nothing, so nothing glides.
	m.MoveCursor(1)
	if m.anim {
		t.Fatal("a step inside the window started a glide")
	}

	m.GotoBottom()
	if !m.anim {
		t.Fatal("G landed at once instead of gliding")
	}
	if m.TakeScrollCmd() == nil {
		t.Fatal("the glide has no frame command")
	}
	if m.TakeScrollCmd() != nil {
		t.Fatal("the frame command was handed out twice")
	}

	// The frames carry the window to the end of the diff and leave the cursor on
	// screen.
	at := time.Now()
	for i := 0; i < 200 && m.anim; i++ {
		at = at.Add(frameInterval)
		next, _ := m.Update(scrollFrameMsg{seq: m.animSeq, at: at})
		m = next
	}
	if m.anim {
		t.Fatal("the glide never settled")
	}
	if got, want := m.YOffset(), m.maxScroll(); got != want {
		t.Fatalf("the glide stopped at %d, want the end of the diff at %d", got, want)
	}
	if !m.cursorInWindow() {
		t.Fatalf("the cursor at %d is off the window at %d", m.CursorRow(), m.YOffset())
	}
	if !m.Smooth() {
		t.Fatal("the setting was turned off by the glide")
	}
}
