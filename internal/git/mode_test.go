package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"samouly.fun/twig/internal/diff"
)

// modeRepo commits an executable script and chdirs into the repository.
func modeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(dir)
	return dir
}

func fileFor(t *testing.T, d *diff.Diff, path string) *diff.File {
	t.Helper()
	f := d.FileByPath(path)
	if f == nil {
		t.Fatalf("%s is missing from the diff", path)
	}
	return f
}

func TestWorktreeDiffStatesAModeChange(t *testing.T) {
	dir := modeRepo(t)
	if err := os.Chmod(filepath.Join(dir, "s.sh"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	d, err := Diff(Source{Kind: SourceWorktree}, Options{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := fileFor(t, d, "s.sh")
	if f.OldMode != "100644" || f.NewMode != "100755" {
		t.Fatalf("modes = %q -> %q, want 100644 -> 100755", f.OldMode, f.NewMode)
	}
	if len(f.Hunks) != 0 {
		t.Fatalf("a chmod carried %d hunks, want none", len(f.Hunks))
	}
}

func TestStagedDiffStatesTheModeOfACreatedFile(t *testing.T) {
	dir := modeRepo(t)
	new := filepath.Join(dir, "new.sh")
	if err := os.WriteFile(new, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := exec.Command("git", "add", "new.sh")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	d, err := Diff(Source{Kind: SourceStaged}, Options{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := fileFor(t, d, "new.sh")
	if f.Kind != diff.Added {
		t.Fatalf("kind = %v, want added", f.Kind)
	}
	if f.NewMode != "100755" {
		t.Fatalf("new mode = %q, want 100755", f.NewMode)
	}
}

func TestStagedDiffStatesTheModeOfADeletedFile(t *testing.T) {
	dir := modeRepo(t)
	for _, args := range [][]string{{"rm", "-q", "gone.txt"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	d, err := Diff(Source{Kind: SourceStaged}, Options{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := fileFor(t, d, "gone.txt")
	if f.Kind != diff.Deleted {
		t.Fatalf("kind = %v, want deleted", f.Kind)
	}
	if f.OldMode != "100644" {
		t.Fatalf("old mode = %q, want 100644", f.OldMode)
	}
}

func TestAPlainModificationStatesNoModes(t *testing.T) {
	dir := modeRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "s.sh"), []byte("#!/bin/sh\necho changed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	d, err := Diff(Source{Kind: SourceWorktree}, Options{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := fileFor(t, d, "s.sh")
	if f.OldMode != "" || f.NewMode != "" {
		t.Fatalf("modes = %q -> %q, want none", f.OldMode, f.NewMode)
	}
}
