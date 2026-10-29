package diff

import (
	"strings"
	"testing"
)

// TestHunkBodyIsBounded pins that a hunk's Lines stop at the end of that hunk.
// They used to run to the end of the document, which made copying a hunk (and
// the pager's "current hunk patch") include every later hunk.
func TestHunkBodyIsBounded(t *testing.T) {
	var oldText, newText strings.Builder
	for i := 0; i < 40; i++ {
		switch i {
		case 5:
			oldText.WriteString("five\n")
			newText.WriteString("FIVE\n")
		case 30:
			oldText.WriteString("thirty\n")
			newText.WriteString("THIRTY\n")
		default:
			oldText.WriteString("same\n")
			newText.WriteString("same\n")
		}
	}

	f := Generate("f.go", Modified, oldText.String(), newText.String(), GenerateOptions{Context: 3})
	if len(f.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(f.Hunks))
	}
	for i, h := range f.Hunks {
		if len(h.Lines) != 8 {
			t.Fatalf("hunk %d body has %d lines, want 8", i, len(h.Lines))
		}
		for _, l := range h.Lines {
			if l.Kind == HunkHeader {
				t.Fatalf("hunk %d body contains a header line", i)
			}
		}
	}

	patch := f.Patch()
	if got := strings.Count(patch, "@@ -"); got != 2 {
		t.Fatalf("patch has %d hunk headers, want 2:\n%s", got, patch)
	}

	// The change markers must land in their own hunk only.
	if !strings.Contains(patch, "-five") || !strings.Contains(patch, "+FIVE") {
		t.Fatalf("first change missing from the patch:\n%s", patch)
	}
	if strings.Contains(patch, "-thirty") != strings.Contains(patch, "+THIRTY") {
		t.Fatalf("second change half missing:\n%s", patch)
	}
}

// TestGenerateEmptySides covers the fast paths a diff dump hits: a file that
// only gained lines, and one that only lost them.
func TestGenerateEmptySides(t *testing.T) {
	added := Generate("new.go", Added, "", "a\nb\nc\n", GenerateOptions{Context: 3})
	if added.Additions != 3 || added.Deletions != 0 {
		t.Fatalf("added = +%d -%d, want +3 -0", added.Additions, added.Deletions)
	}
	if len(added.Hunks) != 1 || len(added.Hunks[0].Lines) != 3 {
		t.Fatalf("added hunks = %d, body = %d", len(added.Hunks), len(added.Hunks[0].Lines))
	}

	deleted := Generate("gone.go", Deleted, "a\nb\nc\n", "", GenerateOptions{Context: 3})
	if deleted.Additions != 0 || deleted.Deletions != 3 {
		t.Fatalf("deleted = +%d -%d, want +0 -3", deleted.Additions, deleted.Deletions)
	}
	if len(deleted.Hunks) != 1 || len(deleted.Hunks[0].Lines) != 3 {
		t.Fatalf("deleted hunks = %d, body = %d", len(deleted.Hunks), len(deleted.Hunks[0].Lines))
	}
}
