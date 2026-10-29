package diffview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"twig/internal/diff"
	"twig/internal/ui/styles"
)

func sampleFile() *diff.File {
	return diff.Generate("main.go", diff.Modified,
		"package main\n\nfunc main() {\n\told()\n}\n",
		"package main\n\nfunc main() {\n\tnew()\n\tmore()\n}\n",
		diff.GenerateOptions{Context: 3})
}

func newSized(split bool, width, height int) Model {
	m := New(false, styles.New())
	m.SetSize(width, height)
	m.SetSplit(split)
	m.SetFile(sampleFile())
	return m
}

func TestViewDimensions(t *testing.T) {
	for _, split := range []bool{false, true} {
		m := newSized(split, 60, 12)
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
	m := newSized(false, 60, 12)
	if !strings.Contains(m.View(), "   1    1") {
		t.Fatalf("expected line numbers in the gutter, got:\n%s", m.View())
	}
}

func TestSplitPairsChangedLines(t *testing.T) {
	m := newSized(true, 60, 12)
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
	m := newSized(false, 60, 12)
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

	m := newSized(true, 80, 12)
	m.SetFile(f)

	before := m.View()
	if !strings.Contains(before, "value := compute") {
		t.Fatalf("split view should show the start of the line:\n%s", before)
	}

	m.vp.SetXOffset(30)
	after := m.View()
	if after == before {
		t.Fatal("scrolling the split view did not move the text")
	}
	if strings.Contains(after, "value := compute") {
		t.Fatalf("split view was not scrolled:\n%s", after)
	}

	m.vp.SetXOffset(60)
	tail := m.View()
	if !strings.Contains(tail, "argumentThree") {
		t.Fatalf("scrolled split view should reveal the line tail:\n%s", tail)
	}

	for i, line := range strings.Split(tail, "\n") {
		if w := lipgloss.Width(line); w != 80 {
			t.Fatalf("line %d width = %d, want 80", i, w)
		}
	}
}

func TestNoHorizontalBarWhenContentFits(t *testing.T) {
	m := newSized(false, 80, 12)
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
	m := newSized(false, 60, 12)
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
	m.SetSize(80, 10)
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
