package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"twig/internal/diff"
)

// TestCommitChangeListAndFileAtCommit checks the two halves of the lazy commit
// view: listing a commit reads tree data only, and loading one file's content
// happens on demand.
func TestCommitChangeListAndFileAtCommit(t *testing.T) {
	h := newRepoHarness(t)
	h.write("keep.txt", "one\ntwo\n")
	h.write("gone.txt", "bye\n")
	h.commitAll("first")

	h.write("keep.txt", "one\nTWO\n")
	h.commitAll("second")

	hash := strings.TrimSpace(h.gitOutput("rev-parse", "HEAD"))
	changes, err := CommitChangeList(hash)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1 (%v)", len(changes), changes)
	}
	if changes[0].Path != "keep.txt" || changes[0].Kind != diff.Modified {
		t.Fatalf("change = %+v, want keep.txt modified", changes[0])
	}

	f, err := FileAtCommit(hash, changes[0], Options{Context: 3})
	if err != nil {
		t.Fatalf("file at commit: %v", err)
	}
	if f.Path() != "keep.txt" {
		t.Fatalf("path = %q", f.Path())
	}
	if f.Additions != 1 || f.Deletions != 1 {
		t.Fatalf("+%d -%d, want +1 -1", f.Additions, f.Deletions)
	}
	patch := f.Patch()
	if !strings.Contains(patch, "-two") || !strings.Contains(patch, "+TWO") {
		t.Fatalf("patch does not contain the change:\n%s", patch)
	}
}

// TestCommitChangeListIncludesAddsAndDeletes covers the kinds the lazy view has
// to describe.
func TestCommitChangeListIncludesAddsAndDeletes(t *testing.T) {
	h := newRepoHarness(t)
	h.write("keep.txt", "one\n")
	h.write("gone.txt", "bye\n")
	h.commitAll("first")

	if err := os.Remove(filepath.Join(h.dir, "gone.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	h.write("added.txt", "new\n")
	h.git("add", "-A")
	h.commitAll("second")

	hash := strings.TrimSpace(h.gitOutput("rev-parse", "HEAD"))
	changes, err := CommitChangeList(hash)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	byPath := make(map[string]diff.ChangeKind, len(changes))
	for _, c := range changes {
		byPath[c.Path] = c.Kind
	}
	if byPath["added.txt"] != diff.Added {
		t.Fatalf("added.txt kind = %v, want added", byPath["added.txt"])
	}
	if byPath["gone.txt"] != diff.Deleted {
		t.Fatalf("gone.txt kind = %v, want deleted", byPath["gone.txt"])
	}
}

// TestCommitDiffDegradesOnMissingBlob checks that content which is not in the
// local object database costs one band, not the whole view: that is what a
// shallow or blobless partial clone looks like.
func TestCommitDiffDegradesOnMissingBlob(t *testing.T) {
	h := newRepoHarness(t)
	h.write("a.txt", "alpha\n")
	h.write("b.txt", "bravo\n")
	h.commitAll("first")
	h.write("a.txt", "ALPHA\n")
	h.write("b.txt", "BRAVO\n")
	h.commitAll("second")

	oldBlob := strings.TrimSpace(h.gitOutput("rev-parse", "HEAD^:a.txt"))
	objectPath := filepath.Join(h.dir, ".git", "objects", oldBlob[:2], oldBlob[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Skipf("the old blob is not a loose object: %v", err)
	}

	d, err := Diff(Source{Kind: SourceCommit, Ref1: "HEAD"}, Options{Context: 3})
	if err != nil {
		t.Fatalf("commit diff failed instead of degrading: %v", err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(d.Files))
	}

	var degraded, loaded int
	for _, f := range d.Files {
		switch {
		case strings.Contains(f.LineText(0), "content not present locally"):
			degraded++
		case f.Additions > 0:
			loaded++
		}
	}
	if degraded != 1 || loaded != 1 {
		t.Fatalf("degraded = %d, loaded = %d, want 1 and 1", degraded, loaded)
	}
}
