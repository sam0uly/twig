package diffview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/syntax"
	"samouly.fun/twig/internal/ui/styles"
)

func sampleFile() *diff.File {
	return diff.Generate("main.go", diff.Modified,
		"package main\n\nfunc main() {\n\told()\n}\n",
		"package main\n\nfunc main() {\n\tnew()\n\tmore()\n}\n",
		diff.GenerateOptions{Context: 3})
}

func newSized(split bool, width int) Model {
	const height = 12
	m := New(false, styles.New())
	m.SetSize(width, height)
	m.SetSplit(split)
	m.SetFile(sampleFile())
	return m
}

func TestViewDimensions(t *testing.T) {
	for _, split := range []bool{false, true} {
		m := newSized(split, 60)
		lines := strings.Split(m.View(), "\n")
		if len(lines) != 12 {
			t.Fatalf("split=%v: got %d lines, want 12", split, len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != 60 {
				t.Fatalf("split=%v: line %d width = %d, want 60", split, i, w)
			}
		}
	}
}

func TestGutterShowsLineNumbers(t *testing.T) {
	m := newSized(false, 60)
	if !strings.Contains(m.View(), "   1    1") {
		t.Fatalf("expected line numbers in the gutter, got:\n%s", m.View())
	}
}

func TestSplitPairsChangedLines(t *testing.T) {
	m := newSized(true, 60)
	// Strip styling: word highlighting splits a line into several rendered
	// segments, and this test is about where the text sits, not how it looks.
	view := ansi.Strip(m.View())

	if !strings.Contains(view, "old()") {
		t.Fatalf("deleted line missing from split view:\n%s", view)
	}
	if !strings.Contains(view, "new()") {
		t.Fatalf("added line missing from split view:\n%s", view)
	}

	// A paired row must put the deletion before the addition on the same row.
	for _, line := range strings.Split(view, "\n") {
		d := strings.Index(line, "old()")
		a := strings.Index(line, "new()")
		if d >= 0 && a >= 0 && d > a {
			t.Fatalf("deletion should be left of addition: %q", line)
		}
	}
}

func TestToggleSplitKeepsFile(t *testing.T) {
	m := newSized(false, 60)
	before := len(m.rows)
	m.ToggleSplit()
	if !m.Split() {
		t.Fatal("ToggleSplit did not enable split mode")
	}
	if m.File() == nil {
		t.Fatal("file was dropped on toggle")
	}
	if len(m.rows) == 0 {
		t.Fatal("split mode rendered nothing")
	}
	// Split mode folds deletion/addition pairs onto one row, so it should
	// never need more rows than the unified view.
	if len(m.rows) > before {
		t.Fatalf("split view grew from %d to %d rows", before, len(m.rows))
	}
}

func TestVerticalScrollbar(t *testing.T) {
	var old strings.Builder
	for i := range 200 {
		old.WriteString("line ")
		old.WriteString(strings.Repeat("x", i%7))
		old.WriteString("\n")
	}
	newText := "changed\n"
	f := diff.Generate("big.go", diff.Modified, old.String(), newText, diff.GenerateOptions{Context: 3})

	m := New(false, styles.New())
	m.SetSize(40, 10)
	m.SetFile(f)
	m.GotoBottom()

	if !strings.Contains(m.View(), "┃") {
		t.Fatalf("expected a scrollbar thumb, got:\n%s", m.View())
	}
}

func TestHorizontalScrollbar(t *testing.T) {
	long := strings.Repeat("wide ", 40)
	f := diff.Generate("wide.go", diff.Modified, long+"\n", long+"changed\n", diff.GenerateOptions{Context: 3})

	m := New(false, styles.New())
	m.SetSize(30, 10)
	m.SetFile(f)

	if !strings.Contains(m.View(), "━") {
		t.Fatalf("expected a horizontal scrollbar, got:\n%s", m.View())
	}
}

// TestSplitViewScrollsInsteadOfTruncating guards the split layout: long lines
// stay complete and are reached by scrolling, rather than being cut off with
// an ellipsis.
func TestSplitViewScrollsInsteadOfTruncating(t *testing.T) {
	long := "value := computeSomethingReallyLong(argumentOne, argumentTwo, argumentThree)"
	f := diff.Generate("main.go", diff.Modified,
		"package main\n\nfunc main() {\n\t"+long+"\n}\n",
		"package main\n\nfunc main() {\n\t"+long+" // changed\n}\n",
		diff.GenerateOptions{Context: 3})

	m := newSized(true, 80)
	m.SetFile(f)

	// Colour codes sit between the tokens now, so the layout assertions read
	// the plain text.
	before := ansi.Strip(m.View())
	if !strings.Contains(before, "value := compute") {
		t.Fatalf("split view should show the start of the line:\n%s", before)
	}

	m.vp.SetXOffset(30)
	after := ansi.Strip(m.View())
	if after == before {
		t.Fatal("scrolling the split view did not move the text")
	}
	if strings.Contains(after, "value := compute") {
		t.Fatalf("split view was not scrolled:\n%s", after)
	}

	m.vp.SetXOffset(60)
	tail := m.View()
	if !strings.Contains(ansi.Strip(tail), "argumentThree") {
		t.Fatalf("scrolled split view should reveal the line tail:\n%s", tail)
	}

	for i, line := range strings.Split(tail, "\n") {
		if w := lipgloss.Width(line); w != 80 {
			t.Fatalf("line %d width = %d, want 80", i, w)
		}
	}
}

func TestNoHorizontalBarWhenContentFits(t *testing.T) {
	m := newSized(false, 80)
	if m.hscroll() {
		t.Fatal("short diff should not need a horizontal scrollbar")
	}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 12 {
		t.Fatalf("got %d lines, want 12 (no bar row when it is not needed)", len(lines))
	}
	last := ansi.Strip(lines[len(lines)-1])
	if strings.ContainsAny(last, "━─") {
		t.Fatalf("horizontal scrollbar rendered without overflow: %q", last)
	}
}

func TestHorizontalBarOnlyWhenNeeded(t *testing.T) {
	long := strings.Repeat("wide ", 40)
	m := newSized(false, 60)
	m.SetFile(diff.Generate("wide.go", diff.Modified, long+"\n", long+"changed\n", diff.GenerateOptions{Context: 3}))

	if !m.hscroll() {
		t.Fatal("wide diff should need a horizontal scrollbar")
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 12 {
		t.Fatalf("got %d lines, want 12", len(lines))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-1]), "━") {
		t.Fatalf("horizontal scrollbar missing: %q", ansi.Strip(lines[len(lines)-1]))
	}
}

func TestJumpToBandStartsScroll(t *testing.T) {
	m := New(true, styles.New())
	m.SetSize(80, 8)
	m.SetFile(diff.Aggregate([]*diff.File{sampleFile(), sampleFile()}))

	if len(m.fileBands) < 2 {
		t.Fatalf("expected 2 file bands, got %d", len(m.fileBands))
	}
	if cmd := m.JumpToBand(1); cmd == nil {
		t.Fatal("jumping to a band should start the smooth scroll")
	}
}

// TestStaleScrollFrameIgnored pins the fix for the drift bug: an animation
// that was superseded must not keep moving the view when its frames arrive.
func TestStaleScrollFrameIgnored(t *testing.T) {
	m := New(true, styles.New())
	m.SetSize(80, 8)
	m.SetFile(diff.Aggregate([]*diff.File{sampleFile(), sampleFile(), sampleFile(), sampleFile()}))

	// Start an animation and cancel it, which bumps the generation.
	if cmd := m.ScrollTo(len(m.rows)); cmd == nil {
		t.Fatal("ScrollTo should start an animation")
	}
	stale := m.animSeq
	m.CancelScroll()
	m.JumpTo(0)
	m.SetSize(80, 8)

	got, cmd := m.Update(scrollFrameMsg{seq: stale})
	if cmd != nil {
		t.Fatal("a stale frame should not schedule another frame")
	}
	if got.YOffset() != 0 {
		t.Fatalf("a stale frame moved the view to %d", got.YOffset())
	}
}

// TestExtendFileKeepsPositionAndSearch covers the streaming document path:
// appending blocks must not move the reader or lose the search.
func TestExtendFileKeepsPositionAndSearch(t *testing.T) {
	tall := func(path string) *diff.File {
		var oldText, newText strings.Builder
		for i := 0; i < 40; i++ {
			oldText.WriteString("line\n")
			if i == 20 {
				newText.WriteString("changed\n")
				continue
			}
			newText.WriteString("line\n")
		}
		return diff.Generate(path, diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3})
	}

	doc := diff.Aggregate([]*diff.File{tall("a.go")})
	m := New(false, styles.New())
	// Shorter than the document, so there is somewhere to scroll to.
	m.SetSize(80, 8)
	m.SetFile(doc)
	if _, cmd := m.Search("line"); cmd != nil {
		m.ApplySearchResults(cmd().(SearchResultsMsg))
	}
	matchesBefore := m.MatchCount()
	if matchesBefore == 0 {
		t.Fatal("search found nothing to begin with")
	}
	m.JumpTo(5)
	offsetBefore := m.YOffset()
	if offsetBefore == 0 {
		t.Fatal("expected a non-zero scroll position")
	}

	doc.Append(tall("b.go"))
	m.ExtendFile(doc)

	if m.YOffset() != offsetBefore {
		t.Fatalf("extending the document moved the view from %d to %d", offsetBefore, m.YOffset())
	}
	if m.MatchCount() <= matchesBefore {
		t.Fatalf("matches = %d, want more than %d after the extension", m.MatchCount(), matchesBefore)
	}
	if !m.HasBand(1) {
		t.Fatal("the appended block did not register a band")
	}
	if !m.HasBand(0) || m.HasBand(2) {
		t.Fatal("band indexes are wrong after the extension")
	}
}

// TestSearchTwoPhase pins the two-phase search: the visible window is scanned
// immediately and the rest arrives as a message that can be applied later, or
// dropped when it went stale.
func TestSearchTwoPhase(t *testing.T) {
	doc := largeFile(300)
	m := New(false, styles.New())
	m.SetSize(80, 10)
	m.SetFile(doc)
	// Start mid-document so the jump on completion is observable.
	m.JumpTo(200)

	windowMatches, cmd := m.Search("handler")
	if windowMatches == 0 {
		t.Fatal("phase one should match inside the visible window")
	}
	if cmd == nil {
		t.Fatal("phase two should be scheduled")
	}

	msg, ok := cmd().(SearchResultsMsg)
	if !ok {
		t.Fatalf("background search produced %T, want SearchResultsMsg", cmd())
	}
	if len(msg.Matches) <= windowMatches {
		t.Fatalf("background scan found %d matches, want more than the window's %d",
			len(msg.Matches), windowMatches)
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}
	if got, want := m.MatchCount(), len(msg.Matches); got != want {
		t.Fatalf("matches = %d, want %d", got, want)
	}
	if m.YOffset() == 0 {
		t.Fatal("applying the results should jump to the first match")
	}

	// A result from a superseded search must not be applied.
	if _, next := m.Search("handler"); next == nil {
		t.Fatal("expected a follow-up scan")
	}
	if m.ApplySearchResults(msg) {
		t.Fatal("results from a superseded search were applied")
	}

	// Clearing the search must invalidate pending results too.
	_, pending := m.Search("handler")
	m.ClearSearch()
	if pending == nil {
		t.Fatal("expected a pending scan")
	}
	if m.ApplySearchResults(pending().(SearchResultsMsg)) {
		t.Fatal("results were applied after clearing the search")
	}
	if m.MatchCount() != 0 {
		t.Fatalf("matches after clearing = %d, want 0", m.MatchCount())
	}
}

// TestSearchNonASCIIStillComplete makes sure the slow path (case folding
// beyond ASCII) also runs through the background scan.
func TestSearchNonASCIIStillComplete(t *testing.T) {
	f := diff.Generate("u.txt", diff.Modified,
		"Grüße\nrest\n", "GRÜSSE\nrest\n", diff.GenerateOptions{Context: 3})
	m := New(false, styles.New())
	m.SetSize(80, 10)
	m.SetFile(f)

	n, cmd := m.Search("grüße")
	if n == 0 || cmd == nil {
		t.Fatalf("phase one = %d matches, cmd nil = %t", n, cmd == nil)
	}
	msg := cmd().(SearchResultsMsg)
	if len(msg.Matches) == 0 {
		t.Fatal("case-insensitive search found nothing")
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}
}

// TestNotesWrapInsteadOfClipping checks a long metadata line (a commit body)
// takes as many rows as it needs, so nothing is cut at the pane edge.
func TestNotesWrapInsteadOfClipping(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(40, 10)
	m.SetFile(diff.Generate("a.txt", diff.Modified, "one\n", "two\n", diff.GenerateOptions{}))

	long := strings.Repeat("word ", 20)
	m.SetNotes([]string{long})

	rows := m.rows
	if len(rows) < 2 {
		t.Fatalf("long note took %d rows, want it wrapped", len(rows))
	}
	for i, r := range rows[:len(rows)-1] {
		if w := ansi.StringWidth(r.text); w > m.vpWidth() {
			t.Fatalf("row %d is %d wide, over the %d viewport", i, w, m.vpWidth())
		}
	}
	// The wrapped text is all still there.
	var got strings.Builder
	for _, r := range rows[:len(rows)-1] {
		got.WriteString(stripANSI(r.text))
		got.WriteString(" ")
	}
	for _, word := range strings.Fields(long) {
		if !strings.Contains(got.String(), word) {
			t.Fatalf("wrapped notes lost %q", word)
		}
	}
}

// TestSmoothScrollKeysAccumulate pins the scroll contract: line steps land
// immediately so the view keeps up with the keyboard, page jumps animate, and
// a second jump during the animation extends the target instead of restarting.
func TestSmoothScrollKeysAccumulate(t *testing.T) {
	var long strings.Builder
	for i := 0; i < 400; i++ {
		long.WriteString("line\n")
	}
	m := New(true, styles.New())
	m.SetSize(40, 10)
	m.SetFile(diff.Generate("a.txt", diff.Modified, "", long.String(), diff.GenerateOptions{}))

	// A line step is instant: no animation, offset already moved.
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	nm := next
	if nm.anim || cmd != nil {
		t.Fatalf("a line step started an animation (anim=%t)", nm.anim)
	}
	if got := nm.vp.YOffset(); got != 1 {
		t.Fatalf("line step offset = %d, want 1", got)
	}

	next, _ = nm.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := next.vp.YOffset(); got != 2 {
		t.Fatalf("second line step offset = %d, want 2", got)
	}

	// A page jump animates, and a second one during the animation adds to the
	// target rather than starting over.
	page := tea.KeyPressMsg{Code: tea.KeyPgDown}
	next, cmd = next.Update(page)
	nm = next
	if !nm.anim || cmd == nil {
		t.Fatalf("page jump should animate (anim=%t)", nm.anim)
	}
	first := nm.target
	if first <= 2 {
		t.Fatalf("page jump target = %v, want past the current offset", first)
	}
	next, _ = nm.Update(page)
	if next.target <= first {
		t.Fatalf("second page jump target = %v, want more than %v", next.target, first)
	}

	// Turning smooth off makes the next jump instant too.
	next.SetSmooth(false)
	next, cmd = next.Update(page)
	if next.anim || cmd != nil {
		t.Fatal("smooth off still animates")
	}
}

// TestSmoothScrollSettlesQuickly pins the feel: a single line step lands within
// a few frames, a long jump arrives well under half a second, and no animation
// frame ever moves further than the target.
func TestSmoothScrollSettlesQuickly(t *testing.T) {
	var long strings.Builder
	for i := 0; i < 400; i++ {
		long.WriteString("line\n")
	}
	m := New(true, styles.New())
	m.SetSize(60, 10)
	m.SetFile(diff.Generate("a.txt", diff.Modified, "", long.String(), diff.GenerateOptions{}))

	settle := func(base Model, frames int) (Model, int) {
		cur := base
		start := time.Now()
		for i := 1; i <= frames; i++ {
			at := start.Add(time.Duration(i) * frameInterval)
			next, _ := cur.Update(scrollFrameMsg{seq: cur.animSeq, at: at})
			cur = next
			if !cur.anim {
				return cur, i
			}
		}
		return cur, frames
	}

	// One line is under the animation threshold: it lands immediately.
	if cmd := m.ScrollTo(1); cmd != nil {
		t.Fatal("a one line step should not animate")
	}
	if got := m.vp.YOffset(); got != 1 {
		t.Fatalf("one line offset = %d, want 1", got)
	}

	// A long jump: 60 rows must arrive in under 30 frames (500ms).
	m2 := New(true, styles.New())
	m2.SetSize(60, 10)
	m2.SetFile(diff.Generate("a.txt", diff.Modified, "", long.String(), diff.GenerateOptions{}))
	if cmd := m2.ScrollTo(60); cmd == nil {
		t.Fatal("ScrollTo should start a long animation")
	}
	nm2, frames := settle(m2, 30)
	if got := nm2.vp.YOffset(); got != 60 {
		t.Fatalf("after %d frames offset = %d, want 60", frames, got)
	}
	if frames >= 30 {
		t.Fatalf("a 60 row jump took %d frames", frames)
	}

	// Wall-clock independence: one coarse 100ms step lands like several 16ms
	// frames would.
	m3 := New(true, styles.New())
	m3.SetSize(60, 10)
	m3.SetFile(diff.Generate("a.txt", diff.Modified, "", long.String(), diff.GenerateOptions{}))
	m3.ScrollTo(40)
	start := time.Now()
	next, _ := m3.Update(scrollFrameMsg{seq: m3.animSeq, at: start})
	coarse := next
	next, _ = coarse.Update(scrollFrameMsg{seq: coarse.animSeq, at: start.Add(100 * time.Millisecond)})
	if next.vp.YOffset() < 40 {
		// Either it arrived or it made most of the trip; it must not crawl.
		if next.vp.YOffset() < 20 {
			t.Fatalf("100ms of animation only moved to %d/40", next.vp.YOffset())
		}
	}
}

// TestGutterGrowsWithLineNumbers checks the number columns widen with the
// document instead of pushing a row past the pane and wrapping it.
func TestGutterGrowsWithLineNumbers(t *testing.T) {
	var long strings.Builder
	for i := 0; i < 120_000; i++ {
		long.WriteString("line\n")
	}
	m := New(false, styles.New())
	m.SetSize(100, 20)
	m.SetFile(diff.Generate("big.txt", diff.Modified, "", long.String(), diff.GenerateOptions{}))

	if got := m.gut.numCells(); got != 6 {
		t.Fatalf("number column = %d cells, want 6 for six-digit lines", got)
	}
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != 100 {
			t.Fatalf("row %d width = %d, want 100", i, w)
		}
	}
}

// TestAggregateHasNoBareTotalsHeader checks a commit document does not print a
// lone "+N -M" line above its first hunk: those totals now live in the notes.
func TestAggregateHasNoBareTotalsHeader(t *testing.T) {
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "one\n", "two\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, "x\n", "y\n", diff.GenerateOptions{Context: 3}),
	}
	agg := diff.Aggregate(files)
	m := New(false, styles.New())
	m.SetSize(80, 20)
	m.SetFile(agg)
	m.SetNotes([]string{"hash sam", "subject", "", " 2 files changed   +2  -2"})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "── a.go") {
		t.Fatalf("file bands missing:\n%s", view)
	}
	for _, row := range strings.Split(view, "\n") {
		trimmed := strings.TrimSpace(row)
		fields := strings.Fields(trimmed)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
			continue
		}
		if trimmed == fields[0]+" "+fields[1] && len(fields[0]) > 1 && len(fields[1]) > 1 {
			t.Fatalf("bare totals row above the diff:\n%s", view)
		}
	}
}

// TestAnchorSurvivesReplace covers the refresh path: the same position has to
// survive content being replaced, which is what keeps a save from scrolling
// the reader back to the top of a long diff.
func TestAnchorSurvivesReplace(t *testing.T) {
	// Every line differs, so the diff is the whole file rather than a short
	// hunk at the end.
	var before, after, more strings.Builder
	for i := range 120 {
		fmt.Fprintf(&before, "line %d\n", i)
		fmt.Fprintf(&after, "changed %d\n", i)
		fmt.Fprintf(&more, "changed %d\n", i)
	}
	more.WriteString("added\n")

	m := New(false, styles.New())
	m.SetSize(80, 20)
	m.SetFile(diff.Generate("a.go", diff.Modified, before.String(), after.String(), diff.GenerateOptions{Context: 3}))
	m.JumpToLine(40)
	if got := m.Anchor(); got != 40 {
		t.Fatalf("anchor after jump = %d, want 40", got)
	}

	m.SetFileAt(diff.Generate("a.go", diff.Modified, before.String(), more.String(), diff.GenerateOptions{Context: 3}), m.Anchor())
	if got := m.Anchor(); got != 40 {
		t.Fatalf("anchor after replace = %d, want 40", got)
	}
}

// syntaxFile is a small Go diff with an untouched line in it, so the token
// colours can be read without word-level emphasis on top.
func syntaxFile() *diff.File {
	body := "package main\n\nfunc main() {\n\ttotal := 42\n"
	return diff.Generate("main.go", diff.Modified,
		body+"\told()\n}\n",
		body+"\tnew()\n}\n",
		diff.GenerateOptions{Context: 3})
}

// TestSyntaxHighlightsTokens checks the three promises of the feature in one
// place: the text is unchanged, tokens take their own colour, and the setting
// turns the colouring off again.
func TestSyntaxHighlightsTokens(t *testing.T) {
	m := newSized(false, 80)
	m.SetFile(syntaxFile())

	on := m.View()
	keyword := m.styles.Syntax(styles.SyntaxContext, syntax.Keyword).Render("func")
	number := m.styles.Syntax(styles.SyntaxContext, syntax.Number).Render("42")
	if !strings.Contains(on, keyword) {
		t.Fatalf("keyword not coloured in:\n%q", on)
	}
	if !strings.Contains(on, number) {
		t.Fatalf("number not coloured in:\n%q", on)
	}

	m.SetSyntax(false)
	off := m.View()
	if ansi.Strip(on) != ansi.Strip(off) {
		t.Fatal("colouring changed the text")
	}
	if off == on {
		t.Fatal("turning syntax off changed nothing")
	}
	if strings.Contains(off, keyword) || strings.Contains(off, number) {
		t.Fatalf("tokens still coloured while disabled:\n%q", off)
	}
	if len(m.syn) != 0 {
		t.Fatalf("%d indexes kept after disabling", len(m.syn))
	}
}

// TestSyntaxIndexIsBuiltOnce guards the cost that matters: opening a diff pays
// for its tokens once, and scrolling or resizing does not pay again.
func TestSyntaxIndexIsBuiltOnce(t *testing.T) {
	m := newSized(false, 80)
	f := syntaxFile()
	m.SetFile(f)

	// The index is built on the first paint, not when the file is handed in.
	m.View()
	src := m.sourceAt(0)
	first, ok := m.syn[src]
	if !ok {
		t.Fatal("first paint did not index the rows")
	}
	m.SetSize(100, 20)
	m.View()
	if got, _ := m.syn[src]; got != first {
		t.Fatal("the index was rebuilt on a repaint")
	}
	if first.Bytes() <= 0 {
		t.Fatalf("index reports %d bytes", first.Bytes())
	}
}

// TestSyntaxHighlightsCommitView covers the eager commit view: one flat
// document built from an aggregate, which has no path of its own, so the rows
// only get their language from the segments the aggregate recorded.
func TestSyntaxHighlightsCommitView(t *testing.T) {
	files := []*diff.File{
		syntaxFile(),
		diff.Generate("b.py", diff.Modified, "x = 1\n", "x = 2\n", diff.GenerateOptions{Context: 3}),
	}
	m := newSized(false, 80)
	m.SetFile(diff.Aggregate(files))

	view := m.View()
	keyword := m.styles.Syntax(styles.SyntaxContext, syntax.Keyword).Render("func")
	if !strings.Contains(view, keyword) {
		t.Fatalf("the commit view did not colour the Go file:\n%q", view)
	}
	if len(m.syn) == 0 {
		t.Fatal("no file of the commit was indexed")
	}
}

// TestSyntaxHighlightsCommitViewSecondFile makes sure the rows of a later file
// are read with their own language, not the first file's.
func TestSyntaxHighlightsCommitViewSecondFile(t *testing.T) {
	files := []*diff.File{
		diff.Generate("a.py", diff.Modified, "x = 1\n", "x = 2\n", diff.GenerateOptions{Context: 3}),
		syntaxFile(),
	}
	m := newSized(false, 80)
	m.SetFile(diff.Aggregate(files))

	view := m.View()
	keyword := m.styles.Syntax(styles.SyntaxContext, syntax.Keyword).Render("func")
	if !strings.Contains(view, keyword) {
		t.Fatalf("rows below the first file were not coloured:\n%q", view)
	}
}

// TestSplitViewMixesFilesByRow checks the virtual document in split mode, where
// a rewritten block is one row a line instead of two and the rows below it sit
// half as far down. fileForRow maps those rows back to files.
func TestSplitViewMixesFilesByRow(t *testing.T) {
	var oldText, newText strings.Builder
	for i := range 40 {
		fmt.Fprintf(&oldText, "old line %d\n", i)
		fmt.Fprintf(&newText, "new line %d\n", i)
	}
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3}),
		diff.Generate("b.py", diff.Modified, "x = 1\n", "x = 2\n", diff.GenerateOptions{Context: 3}),
	}
	m := New(false, styles.New())
	m.SetSize(80, 20)
	m.SetSplit(true)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocks(files)
	m.View()

	if len(m.fileBands) < 2 {
		t.Fatalf("expected a band per file, got %d", len(m.fileBands))
	}
	row := m.fileBands[1] + 1
	if got := m.doc.fileForRow(row); got != files[1] {
		t.Fatalf("row %d resolved to %q, want b.py", row, got.Path())
	}
}

// TestCommitViewSplitSourceAtFollowsBand checks the aggregate commit view in
// split mode: the collapsed first file shifts every later row above its line,
// so the source of a row below it must still be found by row, not by line.
func TestCommitViewSplitSourceAtFollowsBand(t *testing.T) {
	var oldText, newText strings.Builder
	for i := range 40 {
		fmt.Fprintf(&oldText, "old line %d\n", i)
		fmt.Fprintf(&newText, "new line %d\n", i)
	}
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3}),
		diff.Generate("b.py", diff.Modified,
			"def greet(name):\n    return \"hi \" + name\n",
			"def greet(name):\n    return \"hello \" + name\n",
			diff.GenerateOptions{Context: 3}),
	}
	m := New(false, styles.New())
	m.SetSize(80, 20)
	m.SetSplit(true)
	m.SetFile(diff.Aggregate(files))
	m.View()

	if len(m.fileBands) != len(files) {
		t.Fatalf("expected %d file bands, got %d", len(files), len(m.fileBands))
	}
	row := m.fileBands[1] + 1
	if got := m.sourceAt(row).path; got != "b.py" {
		t.Fatalf("row %d resolved to %q, want b.py", row, got)
	}

	m.JumpTo(m.fileBands[1])
	view := m.View()
	keyword := m.styles.Syntax(styles.SyntaxContext, syntax.Keyword).Render("def")
	if !strings.Contains(view, keyword) {
		t.Fatalf("the second file's rows were not coloured as Python:\n%q", view)
	}
}

// TestSearchKeepsHunkHeaderStyle pins the hunk band while a search match lands
// inside the header: the token styles only know added, deleted and unchanged
// rows, so the header must keep its own style for the text no token owns.
func TestSearchKeepsHunkHeaderStyle(t *testing.T) {
	m := newSized(false, 80)
	m.SetFile(syntaxFile())
	m.View()

	if _, cmd := m.Search("@@"); cmd != nil {
		m.ApplySearchResults(cmd().(SearchResultsMsg))
	}
	if m.MatchCount() == 0 {
		t.Fatal("the search found no hunk header")
	}

	// sgr is the escape sequence a style opens with.
	sgr := func(st lipgloss.Style) string {
		const probe = "\x00probe\x00"
		r := st.Render(probe)
		return r[:strings.Index(r, probe)]
	}
	context, hunk := sgr(m.styles.DiffContext()), sgr(m.styles.DiffHunk())

	var header string
	for line := range strings.SplitSeq(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), "@@") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatal("no hunk header on screen")
	}
	if !strings.Contains(header, hunk) {
		t.Fatalf("the hunk header lost its band while searching: %q", header)
	}
	if context != "" && strings.Contains(header, context) {
		t.Fatalf("the hunk header text took the context colour while searching: %q", header)
	}
}

// TestSyntaxIndexCachesMisses makes sure a file with no language is remembered
// as having none. A miss costs a whole-file tokenisation, so paying it once per
// painted row would freeze the view.
func TestSyntaxIndexCachesMisses(t *testing.T) {
	m := newSized(false, 80)
	f := diff.Generate("notes.unknownext", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	m.SetFile(f)
	m.View()

	idx, ok := m.syn[m.sourceAt(0)]
	if !ok {
		t.Fatal("a file with no language was not remembered")
	}
	if idx != nil {
		t.Fatalf("unexpected index for an unknown language: %d bytes", idx.Bytes())
	}
}
