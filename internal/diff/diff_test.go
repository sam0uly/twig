package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func mustParseFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return string(b)
}

func TestParseModifiedFile(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "modified.diff"))
	if len(got.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(got.Files))
	}
	f := got.Files[0]
	if f.Kind != Modified {
		t.Errorf("kind = %q, want M", f.Kind)
	}
	if f.OldPath != "internal/ui/ui.go" || f.NewPath != "internal/ui/ui.go" {
		t.Errorf("paths = %q, %q", f.OldPath, f.NewPath)
	}
	if len(f.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(f.Hunks))
	}

	// Hunk 1: @@ -1,7 +1,5 @@
	h := f.Hunks[0]
	if h.LineIndex != 0 {
		t.Errorf("hunk 1 LineIndex = %d, want 0", h.LineIndex)
	}
	if len(h.Lines) != 9 {
		t.Fatalf("hunk 1 lines = %d, want 9", len(h.Lines))
	}
	first := h.Lines[0]
	if first.Kind != Context || first.Old != 1 || first.New != 1 {
		t.Errorf("first line = %+v, want context 1/1", first)
	}
	var adds, dels int
	for _, l := range h.Lines {
		switch l.Kind {
		case Addition:
			adds++
			if l.Old != 0 {
				t.Errorf("added line has old number %d", l.Old)
			}
		case Deletion:
			dels++
			if l.New != 0 {
				t.Errorf("deleted line has new number %d", l.New)
			}
		}
	}
	if adds != 2 || dels != 5 {
		t.Errorf("hunk 1 adds/dels = %d/%d, want 2/5", adds, dels)
	}

	if f.Additions != 3 || f.Deletions != 5 {
		t.Errorf("file stats = %d/%d, want 3/5", f.Additions, f.Deletions)
	}
	if got.Additions != 3 || got.Deletions != 5 {
		t.Errorf("total stats = %d/%d, want 3/5", got.Additions, got.Deletions)
	}
}

func TestParseMultipleHunksLineNumbers(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "modified.diff"))
	f := got.Files[0]
	h2 := f.Hunks[1]
	// @@ -20,3 +18,4 @@
	if len(h2.Lines) == 0 {
		t.Fatal("hunk 2 has no lines")
	}
	first := h2.Lines[0]
	if first.Kind != Context || first.Old != 20 || first.New != 18 {
		t.Errorf("hunk 2 first line = %+v, want context 20/18", first)
	}
	// Hunk header line must be at the right flat index.
	if f.Lines[h2.LineIndex].Kind != HunkHeader {
		t.Errorf("LineIndex %d does not point at a hunk header", h2.LineIndex)
	}
}

func TestParseNewAndDeletedFiles(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "new_deleted.diff"))
	if len(got.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(got.Files))
	}
	newF, delF := got.Files[0], got.Files[1]

	if newF.Kind != Added {
		t.Errorf("new file kind = %q, want A", newF.Kind)
	}
	if newF.Path() != "docs/README.md" {
		t.Errorf("new file path = %q", newF.Path())
	}
	if newF.Additions != 3 {
		t.Errorf("new file additions = %d, want 3", newF.Additions)
	}
	// Added file: every body line is an addition with no old numbers.
	for _, l := range newF.Lines {
		if l.Kind == Addition && l.Old != 0 {
			t.Errorf("addition line has old number %d", l.Old)
		}
	}

	if delF.Kind != Deleted {
		t.Errorf("deleted file kind = %q, want D", delF.Kind)
	}
	if delF.Path() != "old.go" {
		t.Errorf("deleted file path = %q", delF.Path())
	}
	if delF.Deletions != 2 {
		t.Errorf("deleted file deletions = %d, want 2", delF.Deletions)
	}
}

func TestParseRenameModeChangeBinary(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "special.diff"))
	if len(got.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(got.Files))
	}

	renamed, modeChange, binary := got.Files[0], got.Files[1], got.Files[2]

	if renamed.Kind != Renamed {
		t.Errorf("rename kind = %q, want R", renamed.Kind)
	}
	if renamed.OldPath != "a.txt" || renamed.NewPath != "b.txt" {
		t.Errorf("rename paths = %q -> %q", renamed.OldPath, renamed.NewPath)
	}
	if len(renamed.Hunks) != 1 {
		t.Errorf("rename hunks = %d, want 1", len(renamed.Hunks))
	}

	if modeChange.OldMode != "100644" || modeChange.NewMode != "100755" {
		t.Errorf("modes = %q -> %q", modeChange.OldMode, modeChange.NewMode)
	}

	if !binary.Binary {
		t.Errorf("binary file not marked binary")
	}
	if binary.Path() != "logo.png" {
		t.Errorf("binary file path = %q", binary.Path())
	}
}

func TestParseNoNewlineMarker(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "no_newline.diff"))
	f := got.Files[0]
	found := false
	for _, l := range f.Lines {
		if l.Kind == NoNewline {
			found = true
		}
	}
	if !found {
		t.Error("no \\ No newline marker parsed")
	}
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "\n", "nothing to see here\n"} {
		got := Parse(in)
		if len(got.Files) != 0 {
			t.Errorf("Parse(%q) files = %d, want 0", in, len(got.Files))
		}
	}
}

func TestFilePatchRoundTrip(t *testing.T) {
	t.Parallel()

	src := mustParseFile(t, "modified.diff")
	got := Parse(src)
	patch := got.Files[0].Patch()
	// Re-parse the generated patch; the hunk content should survive.
	reparsed := Parse(patch)
	if len(reparsed.Files) != 1 {
		t.Fatalf("reparsed files = %d, want 1", len(reparsed.Files))
	}
	if reparsed.Files[0].Additions != got.Files[0].Additions ||
		reparsed.Files[0].Deletions != got.Files[0].Deletions {
		t.Errorf("reparsed stats = %d/%d, want %d/%d",
			reparsed.Files[0].Additions, reparsed.Files[0].Deletions,
			got.Files[0].Additions, got.Files[0].Deletions)
	}
}

func TestParseSingleLineHunkHeader(t *testing.T) {
	t.Parallel()

	// Hunk headers of the form "@@ -5 +5 @@" (count implied 1) must parse.
	got := Parse("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -5 +5 @@\n-x\n+x2\n")
	f := got.Files[0]
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(f.Hunks))
	}
	if len(f.Hunks[0].Lines) != 2 {
		t.Fatalf("hunk lines = %d, want 2", len(f.Hunks[0].Lines))
	}
	if f.Hunks[0].Lines[0].Old != 5 || f.Hunks[0].Lines[0].New != 0 {
		t.Errorf("deleted line numbers = %d/%d, want 5/0",
			f.Hunks[0].Lines[0].Old, f.Hunks[0].Lines[0].New)
	}
	if f.Hunks[0].Lines[1].Old != 0 || f.Hunks[0].Lines[1].New != 5 {
		t.Errorf("added line numbers = %d/%d, want 0/5",
			f.Hunks[0].Lines[1].Old, f.Hunks[0].Lines[1].New)
	}
}

func TestDiffGitLineWithSpacesInPath(t *testing.T) {
	t.Parallel()

	got := Parse("diff --git a/my file.txt b/my file.txt\n--- a/my file.txt\n+++ b/my file.txt\n@@ -1 +1 @@\n-a\n+b\n")
	f := got.Files[0]
	if f.OldPath != "my file.txt" || f.NewPath != "my file.txt" {
		t.Errorf("paths = %q -> %q", f.OldPath, f.NewPath)
	}
}
