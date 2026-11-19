package diff

import (
	"fmt"
	"strings"
	"testing"
)

// wideChange is a file with one line replaced near the middle and another
// replaced near the end, with an insertion in front of the tail change. It is
// the shape that used to lose the hunk's trailing context: two edits close
// enough to share one hunk, wide context, and a file that continues below.
func wideChange(lines int) (oldText, newText string) {
	old := make([]string, lines)
	for i := range old {
		old[i] = fmt.Sprintf("line %d", i+1)
	}
	// One line becomes two, which shifts everything below it in the new file.
	nw := append([]string{}, old[:lines/2-1]...)
	nw = append(nw, "line a", "line b")
	nw = append(nw, old[lines/2:]...)
	// Replace the second to last line, so one line of the file is left below it.
	nw[len(nw)-2] = "changed near the end"
	return strings.Join(old, "\n") + "\n", strings.Join(nw, "\n") + "\n"
}

// A hunk must never end on a change while the file continues below it: a patch
// reader has nothing to anchor the tail on, and git apply refuses such a patch.
func TestHunksKeepTrailingContext(t *testing.T) {
	for _, lines := range []int{12, 23, 40} {
		for _, context := range []int{1, 3, 8, 20} {
			t.Run(fmt.Sprintf("%d lines, context %d", lines, context), func(t *testing.T) {
				old, nw := wideChange(lines)
				f := Generate("f.txt", Modified, old, nw, GenerateOptions{Context: context})
				if len(f.Hunks) == 0 {
					t.Fatal("no hunk was generated")
				}
				for i, h := range f.Hunks {
					if len(h.Lines) == 0 {
						continue
					}
					tail := h.Lines[len(h.Lines)-1]
					if tail.Kind == Context {
						continue
					}
					// Ending on a change is right only at the end of the file.
					if tail.Old == strings.Count(old, "\n") || tail.New == strings.Count(nw, "\n") {
						continue
					}
					t.Fatalf("hunk %d ends on a change with lines below it: %q (old %d, new %d)",
						i, tail.Text, tail.Old, tail.New)
				}
			})
		}
	}
}

// The last hunk has to keep showing what comes after its final change, which is
// what a reader scrolls to and what a patch needs to apply.
func TestLastHunkShowsTheLinesBelowTheChange(t *testing.T) {
	old, nw := wideChange(23)
	f := Generate("f.txt", Modified, old, nw, GenerateOptions{Context: 8})

	last := f.Hunks[len(f.Hunks)-1]
	tail := last.Lines[len(last.Lines)-1]
	if tail.Kind != Context {
		t.Fatalf("the last hunk ends on %q, want a context line", tail.Text)
	}
	if want := fmt.Sprintf("line %d", 23); tail.Text != " "+want {
		t.Fatalf("the last hunk ends on %q, want %q", tail.Text, want)
	}
}
