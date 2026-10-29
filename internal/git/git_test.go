package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoHarness drives a scratch repository through the real git binary, which
// is exactly the "operated outside twig" case the status engine must survive.
type repoHarness struct {
	t   *testing.T
	dir string
}

func newRepoHarness(t *testing.T) *repoHarness {
	t.Helper()
	h := &repoHarness{t: t, dir: t.TempDir()}
	t.Chdir(h.dir)
	h.git("init", "--initial-branch=main")
	return h
}

func (h *repoHarness) git(args ...string) {
	h.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gitOutput runs git and returns its standard output.
func (h *repoHarness) gitOutput(args ...string) string {
	h.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.Output()
	if err != nil {
		h.t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func (h *repoHarness) write(name, content string) {
	h.t.Helper()
	full := filepath.Join(h.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatalf("write %s: %v", name, err)
	}
}

func (h *repoHarness) commitAll(msg string) {
	h.t.Helper()
	h.git("add", ".")
	h.git("commit", "-m", msg)
}

// newTestRepo creates a temporary git repository, chdirs into it, and seeds
// it with an initial commit plus working tree changes.
func newTestRepo(t *testing.T) context.Context {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	run("init", "--initial-branch=main")
	mustWrite("a.txt", "one\ntwo\n")
	mustWrite("b.txt", "hello\n")
	run("add", ".")
	run("commit", "-m", "initial")

	// Unstaged change in a.txt, staged change in b.txt.
	mustWrite("a.txt", "one\nTWO\nthree\n")
	mustWrite("b.txt", "hello world\n")
	run("add", "b.txt")

	return context.Background()
}

func TestCheckRepository(t *testing.T) {
	newTestRepo(t)
	if err := CheckRepository(); err != nil {
		t.Fatalf("CheckRepository: %v", err)
	}
}

func TestCheckRepositoryOutside(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := CheckRepository(); err == nil {
		t.Fatal("CheckRepository succeeded outside a repository")
	}
}

func TestDiffWorktreeAndStaged(t *testing.T) {
	newTestRepo(t)

	got, err := Diff(Source{Kind: SourceWorktree}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	f := got.FileByPath("a.txt")
	if f == nil {
		t.Fatalf("a.txt missing from worktree diff; files: %d", len(got.Files))
	}
	if f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("a.txt stats = %d/%d, want 2/1", f.Additions, f.Deletions)
	}

	staged, err := Diff(Source{Kind: SourceStaged}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff staged: %v", err)
	}
	if staged.FileByPath("a.txt") != nil {
		t.Error("a.txt should not appear in staged diff")
	}
	if staged.FileByPath("b.txt") == nil {
		t.Error("b.txt missing from staged diff")
	}
}

func TestDiffCommitAndRange(t *testing.T) {
	newTestRepo(t)

	commits, err := Log(10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(commits))
	}
	if commits[0].Subject != "initial" {
		t.Errorf("subject = %q", commits[0].Subject)
	}
	if commits[0].Short == "" || len(commits[0].Hash) != 40 {
		t.Errorf("hash = %q, short = %q", commits[0].Hash, commits[0].Short)
	}

	got, err := Diff(Source{Kind: SourceCommit, Ref1: "HEAD"}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff commit: %v", err)
	}
	if len(got.Files) != 2 {
		t.Errorf("commit diff files = %d, want 2", len(got.Files))
	}

	rg, err := Diff(Source{Kind: SourceRange, Ref1: "HEAD", Ref2: "HEAD"}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff range: %v", err)
	}
	if len(rg.Files) != 0 {
		t.Errorf("empty range diff files = %d, want 0", len(rg.Files))
	}
}

func TestBranchAndStaging(t *testing.T) {
	newTestRepo(t)

	if b := Branch(); b != "main" {
		t.Errorf("branch = %q, want main", b)
	}

	if err := Stage("a.txt"); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	staged, err := Diff(Source{Kind: SourceStaged}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff staged: %v", err)
	}
	if staged.FileByPath("a.txt") == nil {
		t.Error("a.txt missing from staged diff after staging")
	}

	if err := Unstage("a.txt"); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	staged, err = Diff(Source{Kind: SourceStaged}, Options{Context: 3})
	if err != nil {
		t.Fatalf("Diff staged: %v", err)
	}
	if staged.FileByPath("a.txt") != nil {
		t.Error("a.txt still staged after unstaging")
	}
}

func TestSourceDescribe(t *testing.T) {
	tests := []struct {
		src  Source
		want string
	}{
		{Source{Kind: SourceWorktree}, "worktree"},
		{Source{Kind: SourceStaged}, "staged"},
		{Source{Kind: SourceCommit, Ref1: "abc123"}, "abc123"},
		{Source{Kind: SourceRange, Ref1: "main", Ref2: "feat"}, "main..feat"},
	}
	for _, tt := range tests {
		if got := tt.src.Describe(); got != tt.want {
			t.Errorf("Describe(%+v) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestStatusFileDiffAndCommit(t *testing.T) {
	newTestRepo(t)

	staged, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(staged) != 1 || staged[0].Path != "b.txt" || staged[0].Letter != "M" {
		t.Errorf("staged = %+v, want b.txt M", staged)
	}
	if len(unstaged) != 1 || unstaged[0].Path != "a.txt" || unstaged[0].Letter != "M" {
		t.Errorf("unstaged = %+v, want a.txt M", unstaged)
	}

	f, err := FileDiff("a.txt", false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("file diff stats = %d/%d, want 2/1", f.Additions, f.Deletions)
	}

	f, err = FileDiff("b.txt", true, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff staged: %v", err)
	}
	if f.Additions != 1 || f.Deletions != 1 {
		t.Errorf("staged diff stats = %d/%d, want 1/1", f.Additions, f.Deletions)
	}

	if _, err := CommitChanges(""); err == nil {
		t.Error("CommitChanges with empty message should fail")
	}

	hash, err := CommitChanges("test commit")
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(hash) != 40 {
		t.Errorf("hash = %q, want 40 chars", hash)
	}

	staged, _, err = Status()
	if err != nil {
		t.Fatalf("Status after commit: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("staged after commit = %+v, want empty", staged)
	}
}

// TestSnapshotKinds covers what the hand-written status engine has to get
// right on its own: untracked files, ignored files, deletions and renames.
func TestSnapshotKinds(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	run("init", "--initial-branch=main")
	write(".gitignore", "*.log\n")
	write("keep.txt", "keep\n")
	write("old.txt", "moved content\n")
	write("gone.txt", "delete me\n")
	run("add", ".")
	run("commit", "-m", "initial")

	// A staged rename, an unstaged deletion, an untracked file and an
	// ignored file.
	run("mv", "old.txt", "new.txt")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatalf("remove gone.txt: %v", err)
	}
	write("untracked.txt", "hello\n")
	write("noise.log", "ignored\n")

	staged, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(staged) != 1 || staged[0].Path != "new.txt" || staged[0].Letter != "R" {
		t.Errorf("staged = %+v, want new.txt R", staged)
	} else if staged[0].OldPath != "old.txt" || staged[0].DisplayPath() != "old.txt → new.txt" {
		t.Errorf("rename display = %q / %q", staged[0].OldPath, staged[0].DisplayPath())
	}

	got := map[string]FileStatus{}
	for _, f := range unstaged {
		got[f.Path] = f
	}
	if f, ok := got["gone.txt"]; !ok || f.Letter != "D" {
		t.Errorf("gone.txt = %+v (present %t), want deletion", f, ok)
	}
	if f, ok := got["untracked.txt"]; !ok || !f.Untracked || f.Letter != "?" {
		t.Errorf("untracked.txt = %+v (present %t), want untracked", f, ok)
	}
	if _, ok := got["noise.log"]; ok {
		t.Error("ignored file leaked into the status")
	}
	if len(unstaged) != 2 {
		t.Errorf("unstaged = %+v, want exactly the deletion and the untracked file", unstaged)
	}

	// The rename must diff as the same content under two names.
	f, err := FileDiff("new.txt", true, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff staged rename: %v", err)
	}
	if f.OldPath != "old.txt" || f.NewPath != "new.txt" {
		t.Errorf("rename paths = %q..%q", f.OldPath, f.NewPath)
	}
	if f.Additions != 0 || f.Deletions != 0 {
		t.Errorf("rename stats = %d/%d, want 0/0", f.Additions, f.Deletions)
	}

	// Deleted files diff as removals of their old content.
	f, err = FileDiff("gone.txt", false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff deleted: %v", err)
	}
	if f.Deletions != 1 {
		t.Errorf("deletion stats = %d/%d, want 0/1", f.Additions, f.Deletions)
	}

	// Renewed snapshots must not drift: a second call sees the same state.
	staged2, unstaged2, err := Status()
	if err != nil {
		t.Fatalf("second Status: %v", err)
	}
	if len(staged2) != len(staged) || len(unstaged2) != len(unstaged) {
		t.Errorf("second status = %d/%d, want %d/%d",
			len(staged2), len(unstaged2), len(staged), len(unstaged))
	}
}

// TestStatusIgnoresDirectories pins the fix for phantom staged deletions:
// HEAD's tree walker yields subtree entries too, and those are not files.
func TestStatusIgnoresDirectories(t *testing.T) {
	h := newRepoHarness(t)
	h.write("main.go", "package main\n")
	h.write("internal/git/status.go", "package git\n")
	h.write("internal/ui/tui.go", "package ui\n")
	h.write("docs/guide/intro.md", "# guide\n")
	h.write("docs/guide/deep/more.md", "# more\n")
	h.commitAll("initial")

	staged, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("staged = %+v, want none: directories are not tracked files", staged)
	}
	if len(unstaged) != 0 {
		t.Fatalf("unstaged = %+v, want none", unstaged)
	}

	// Nothing may point at a tree object either: every listed file must diff.
	staged, _, err = Status()
	if err != nil {
		t.Fatalf("second Status: %v", err)
	}
	if len(staged) != 0 {
		t.Fatalf("second staged = %+v, want none", staged)
	}
}

// TestStatusPicksUpExternalIndexChange covers the repository being used from
// outside twig: the session must re-read the index instead of trusting the
// decode it made earlier.
func TestStatusPicksUpExternalIndexChange(t *testing.T) {
	h := newRepoHarness(t)
	h.write("a.txt", "one\n")
	h.commitAll("initial")

	// Open the session and snapshot a clean tree first.
	if staged, unstaged, err := Status(); err != nil || len(staged) != 0 || len(unstaged) != 0 {
		t.Fatalf("initial status = %+v / %+v (%v), want clean", staged, unstaged, err)
	}

	// Stage from another process, exactly like a second terminal would.
	h.write("a.txt", "one\ntwo\n")
	h.git("add", "a.txt")

	staged, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(staged) != 1 || staged[0].Path != "a.txt" || staged[0].Letter != "M" {
		t.Fatalf("staged = %+v, want a.txt M", staged)
	}
	if len(unstaged) != 0 {
		t.Fatalf("unstaged = %+v, want none", unstaged)
	}

	// A commit outside twig must move HEAD as well.
	h.git("commit", "-m", "two")
	commits, err := Log(10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 2 || commits[0].Subject != "two" {
		t.Fatalf("commits = %+v, want the new commit first", commits)
	}
}

// TestNestedGitFileIsNotUntracked covers vendored checkouts, where .git is a
// file rather than a directory.
func TestNestedGitFileIsNotUntracked(t *testing.T) {
	h := newRepoHarness(t)
	h.write("keep.txt", "keep\n")
	h.commitAll("initial")

	h.write("vendor/pkg/.git", "gitdir: ../../.git/modules/pkg\n")
	h.write("vendor/pkg/main.go", "package pkg\n")

	_, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	paths := make(map[string]FileStatus, len(unstaged))
	for _, f := range unstaged {
		paths[f.Path] = f
	}
	if _, ok := paths["vendor/pkg/.git"]; ok {
		t.Errorf(".git file leaked into the status: %+v", unstaged)
	}
	if f, ok := paths["vendor/pkg/main.go"]; !ok || !f.Untracked {
		t.Errorf("vendored source = %+v (present %t), want untracked", f, ok)
	}
}

// TestModeOnlyChangeIsListed: a chmod is a change, but it has no line diff.
func TestModeOnlyChangeIsListed(t *testing.T) {
	h := newRepoHarness(t)
	h.write("run.sh", "#!/bin/sh\necho hi\n")
	h.commitAll("initial")

	if err := os.Chmod(filepath.Join(h.dir, "run.sh"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, unstaged, err := Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(unstaged) != 1 || unstaged[0].Path != "run.sh" || unstaged[0].Letter != "M" {
		t.Fatalf("unstaged = %+v, want run.sh M", unstaged)
	}

	f, err := FileDiff("run.sh", false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if len(f.Hunks) != 0 {
		t.Errorf("mode change produced %d hunks, want none", len(f.Hunks))
	}
	if len(f.Lines) != 1 || !strings.Contains(strings.ToLower(f.Lines[0].Text), "mode") {
		t.Fatalf("lines = %+v, want a single mode notice", f.Lines)
	}
}
