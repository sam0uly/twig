package diff

import (
	"strings"
	"testing"
	"time"
)

// TestHunkSlidingImprovesBoundaries checks that sliding a hunk into a
// blank-line separated block never moves the change rows and always ends up at
// least as natural a boundary as the naive context window.
func TestHunkSlidingImprovesBoundaries(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 12; i++ {
		oldB.WriteString("\tstatement\n")
		newB.WriteString("\tstatement\n")
	}
	oldB.WriteString("\tchanged before\n")
	newB.WriteString("\tchanged after\n")
	// Five more statements, then a blank line: the blank sits two rows past the
	// naive window, which is exactly as far as the slider may travel.
	for i := 0; i < 5; i++ {
		oldB.WriteString("\tstatement\n")
		newB.WriteString("\tstatement\n")
	}
	oldB.WriteString("\n")
	newB.WriteString("\n")
	oldB.WriteString("}\n")
	newB.WriteString("}\n")

	old, new := indexText(oldB.String()), indexText(newB.String())
	edits := lineDiff(old, new, false, time.Now().Add(time.Second))
	valid := validEdits(edits, old, new)
	if len(valid) == 0 {
		t.Fatal("fixture produced no edits")
	}

	naive := naiveWindows(valid, 3)
	if len(naive) != 1 {
		t.Fatalf("naive windows = %d, want 1", len(naive))
	}
	slid := hunkWindows(edits, old, new, 3)
	if len(slid) != 1 {
		t.Fatalf("slid windows = %d, want 1", len(slid))
	}

	before, after := windowScore(naive[0], valid, old, new), windowScore(slid[0], valid, old, new)
	if after < before {
		t.Fatalf("sliding made the boundary worse: %d -> %d", before, after)
	}
	if after == before {
		t.Fatalf("fixture has a better boundary but sliding did not find it (score %d)", before)
	}

	// The change rows must be the same: sliding only ever crosses context.
	if got, want := changeRows(valid, slid[0]), changeRows(valid, naive[0]); got != want {
		t.Fatalf("change rows moved: %d -> %d", want, got)
	}
}

// windowScore rates a window as it stands, using the same heuristic the slider
// maximises.
func windowScore(w hunkWindow, valid []edit, old, new text) int {
	return slideScore(0, w, valid, old, new)
}

// changeRows counts the non-context rows inside a window.
func changeRows(valid []edit, w hunkWindow) int {
	n := 0
	for _, e := range valid[w.start:w.end] {
		if e.kind != editEqual {
			n++
		}
	}
	return n
}

// TestHunkSlidingStaysInBounds checks that sliding never reorders, overlaps or
// empties hunks.
func TestHunkSlidingStaysInBounds(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 400; i++ {
		switch i % 20 {
		case 0:
			oldB.WriteString("changed before\n")
			newB.WriteString("changed after\n")
		case 10:
			oldB.WriteString("\n")
			newB.WriteString("\n")
		default:
			oldB.WriteString("\tline\n")
			newB.WriteString("\tline\n")
		}
	}

	old, new := indexText(oldB.String()), indexText(newB.String())
	edits := lineDiff(old, new, false, time.Now().Add(time.Second))
	valid := validEdits(edits, old, new)
	windows := hunkWindows(edits, old, new, 3)
	if len(windows) < 2 {
		t.Fatalf("windows = %d, want several", len(windows))
	}

	prevEnd := -1
	for i, w := range windows {
		if w.start <= prevEnd {
			t.Fatalf("window %d starts at %d, previous ended at %d", i, w.start, prevEnd)
		}
		if w.end <= w.start || w.end > len(valid) {
			t.Fatalf("window %d is [%d,%d) over %d rows", i, w.start, w.end, len(valid))
		}
		if changeRows(valid, w) == 0 {
			t.Fatalf("window %d has no change rows", i)
		}
		prevEnd = w.end
	}
}

// TestWordDiffSkipsHugeRuns pins the word-diff budget: a wholesale rewrite keeps
// its plain wash instead of spending the page's time pairing thousands of lines.
func TestWordDiffSkipsHugeRuns(t *testing.T) {
	lines := make([]Line, 0, 2*maxPairRows+2)
	for i := 0; i < maxPairRows+1; i++ {
		lines = append(lines, Line{Kind: Deletion, Text: "-old line " + strings.Repeat("x", 3)})
	}
	for i := 0; i < maxPairRows+1; i++ {
		lines = append(lines, Line{Kind: Addition, Text: "+new line " + strings.Repeat("y", 3)})
	}

	annotateLines(lines, false, false)
	for i, l := range lines {
		if len(l.WordRanges()) != 0 {
			t.Fatalf("line %d got word spans from a skipped run", i)
		}
	}
}

// TestWordDiffSkipsLongLines checks the length cap: minified content must not be
// tokenized.
func TestWordDiffSkipsLongLines(t *testing.T) {
	long := strings.Repeat("a=b;", maxWordLine/4+10)
	lines := []Line{
		{Kind: Deletion, Text: "-" + long},
		{Kind: Addition, Text: "+" + long + "c=d;"},
	}
	annotateLines(lines, false, false)
	for i, l := range lines {
		if len(l.WordRanges()) != 0 {
			t.Fatalf("line %d got word spans from an over-long line", i)
		}
	}
}

// TestWordDiffBudgeted checks that an exhausted budget leaves the document valid
// instead of half-annotated garbage: spans stay inside their line.
func TestWordDiffBudgeted(t *testing.T) {
	var lines []Line
	for i := 0; i < 400; i++ {
		lines = append(
			lines,
			Line{Kind: Deletion, Text: "-func handler() error { return nil }"},
			Line{Kind: Addition, Text: "+func handler() error { return err }"},
		)
	}

	annotateWithin(lines, false, false, time.Now().Add(-time.Second))
	for i, l := range lines {
		if len(l.WordRanges()) != 0 {
			t.Fatalf("line %d was annotated after the budget expired", i)
		}
	}

	annotateLines(lines, false, false)
	annotated := 0
	for i, l := range lines {
		for _, s := range l.WordRanges() {
			if s[0] < 0 || s[1] > len(l.Text) || s[0] > s[1] {
				t.Fatalf("line %d has span %v outside %q", i, s, l.Text)
			}
		}
		if len(l.WordRanges()) > 0 {
			annotated++
		}
	}
	if annotated == 0 {
		t.Fatal("no line was annotated")
	}
}
