package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
)

// mergeRepo leaves a conflicted merge in place and chdirs into it.
func mergeRepo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)
		c := exec.Command("git", full...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeFile := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	gitRun("init", "-q", ".")
	writeFile("a\nb\nc\n")
	gitRun("add", ".")
	gitRun("commit", "-qm", "base")
	gitRun("checkout", "-qb", "side")
	writeFile("a\nSIDE\nc\n")
	gitRun("commit", "-qam", "side")
	gitRun("checkout", "-q", "-")
	writeFile("a\nMAIN\nc\n")
	gitRun("commit", "-qam", "main")

	// The merge is expected to fail; that is the state under test.
	c := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "merge", "side")
	c.Dir = dir
	if err := c.Run(); err == nil {
		t.Fatal("the merge did not conflict")
	}

	t.Chdir(dir)
}

func TestConflictedPathShowsTheWorktreeAsItStands(t *testing.T) {
	mergeRepo(t)

	d, err := Diff(Source{Kind: SourceWorktree}, Options{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := fileFor(t, d, "f.txt")
	if f.Kind != diff.Unmerged {
		t.Fatalf("kind = %v, want unmerged", f.Kind)
	}

	var text strings.Builder
	for _, line := range f.Lines {
		text.WriteString(line.Text + "\n")
	}
	for _, want := range []string{"<<<<<<< HEAD", "MAIN", "=======", "SIDE", ">>>>>>> side"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("the file lost %q:\n%s", want, text.String())
		}
	}
}

func TestConflictedPathIsListedAsUnmerged(t *testing.T) {
	mergeRepo(t)

	st, err := WorkingTree()
	if err != nil {
		t.Fatalf("WorkingTree: %v", err)
	}
	for _, row := range append(st.Staged, st.Unstaged...) {
		if row.Path == "f.txt" {
			if row.Letter != "U" {
				t.Fatalf("letter = %q, want U", row.Letter)
			}
			return
		}
	}
	t.Fatalf("the conflicted path is missing from the listing: %+v", st)
}
