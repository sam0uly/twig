package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
)

// sourceRepo builds a repository with one commit behind it and one file left
// modified, so a commit's diff and the working tree cannot be confused.
func sourceRepo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@twig.local")
	run("config", "user.name", "Test")
	write("a.go", "package main\n\nvar A = 1\n")
	write("b.go", "package main\n\nvar B = 1\n")
	run("add", "-A")
	run("commit", "-qm", "seed")

	// HEAD changes one file; the working tree changes the other.
	write("a.go", "package main\n\nvar A = 2\n")
	run("add", "a.go")
	run("commit", "-qm", "touch a")
	write("b.go", "package main\n\nvar B = 2\n")

	t.Chdir(dir)
}

// listedPaths walks the selectable rows of the sidebar. Section headers are
// skipped by the walk, which is what a reader sees too.
func listedPaths(m model) []string {
	m.setSize()
	m.files.Select(0)
	var paths []string
	for range 64 {
		if fs, _ := m.files.SelectedFile(); fs.Path != "" {
			paths = append(paths, fs.Path)
		}
		if !m.files.MoveSelectable(1) {
			break
		}
	}
	return paths
}

func sourceModel(t *testing.T, src git.Source) model {
	t.Helper()
	m := newModel(Options{Config: config.Default(), Source: src}, "main", "twig")
	m.width, m.height = 120, 30
	m.loaded = true
	m.setSize()
	return m
}

// The window used to say "--staged" while showing the working tree, so the tab
// and the panes disagreed about what was on screen.
func TestStagedSourceHidesTheWorkingTreeRows(t *testing.T) {
	sourceRepo(t)

	m := sourceModel(t, git.Source{Kind: git.SourceWorktree})
	next, _ := m.Update(statusLoadedMsg{
		staged:   []git.FileStatus{{Path: "a.go", Letter: "M", Staged: true}},
		unstaged: []git.FileStatus{{Path: "b.go", Letter: "M"}},
	})
	if got := listedPaths(next.(model)); len(got) != 2 {
		t.Fatalf("worktree listing = %v, want both sides", got)
	}

	m = sourceModel(t, git.Source{Kind: git.SourceStaged})
	next, _ = m.Update(statusLoadedMsg{
		staged:   []git.FileStatus{{Path: "a.go", Letter: "M", Staged: true}},
		unstaged: []git.FileStatus{{Path: "b.go", Letter: "M"}},
	})
	if got := listedPaths(next.(model)); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("staged listing = %v, want only the index side", got)
	}
}

// A commit asked for on the command line is the diff that must be shown, not
// the working tree the viewer happens to be sitting in.
func TestPinnedCommitShowsThatCommit(t *testing.T) {
	sourceRepo(t)

	m := sourceModel(t, git.Source{Kind: git.SourceCommit, Ref1: "HEAD"})
	msg := loadPinnedCmd(m.src, m.diffOptions())()

	next, _ := m.Update(msg)
	got := next.(model)

	if !got.pinned {
		t.Fatal("a commit source is not marked read only")
	}
	if paths := listedPaths(got); len(paths) != 1 || paths[0] != "a.go" {
		t.Fatalf("listing = %v, want the commit's single file", paths)
	}
	if got.view.File() == nil {
		t.Fatal("the pane holds no diff")
	}
	if got.d == nil || got.d.Additions == 0 {
		t.Fatalf("totals = %+v, want the commit's line counts", got.d)
	}
}

// A range is the same kind of document as a commit: read only, no staging.
func TestPinnedRangeIsReadOnly(t *testing.T) {
	sourceRepo(t)

	m := sourceModel(t, git.Source{Kind: git.SourceRange, Ref1: "HEAD~1", Ref2: "HEAD"})
	next, _ := m.Update(loadPinnedCmd(m.src, m.diffOptions())())
	got := next.(model)

	if paths := listedPaths(got); len(paths) != 1 || paths[0] != "a.go" {
		t.Fatalf("range listing = %v, want a.go", paths)
	}

	if stageNext, _ := got.toggleStage(true); stageNext.(model).message != readOnlyNotice {
		t.Fatalf("staging a pinned diff said %q", stageNext.(model).message)
	}
	if composeNext, _ := got.startCompose(); composeNext.(model).message != readOnlyNotice {
		t.Fatalf("committing a pinned diff said %q", composeNext.(model).message)
	}
	if _, cmd := got.startPush(); cmd != nil {
		t.Fatal("a pinned diff can be pushed")
	}
}

// The failures have to land on the fatal screen rather than a blank pane.
func TestBadRefFailsBeforeTheViewerOpens(t *testing.T) {
	sourceRepo(t)

	m := sourceModel(t, git.Source{Kind: git.SourceCommit, Ref1: "not-a-ref"})
	next, _ := m.Update(loadPinnedCmd(m.src, m.diffOptions())())

	got := next.(model)
	if got.state != stateFatal {
		t.Fatalf("state = %v, want stateFatal", got.state)
	}
	if got.err == nil {
		t.Fatal("no error was kept for the fatal screen")
	}
}

// A path after -- narrows the listing the same way it narrows a document.
func TestPathsNarrowTheListing(t *testing.T) {
	sourceRepo(t)

	m := sourceModel(t, git.Source{Kind: git.SourceWorktree, Paths: []string{"a.go"}})
	next, _ := m.Update(statusLoadedMsg{
		staged:   []git.FileStatus{{Path: "a.go", Letter: "M", Staged: true}},
		unstaged: []git.FileStatus{{Path: "b.go", Letter: "M"}},
	})

	if got := listedPaths(next.(model)); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("listing = %v, want only a.go", got)
	}
}

// The pager hands the viewer a diff with no repository behind it: the listing,
// the pane and the header all have to work from the document alone.
func TestDocumentNeedsNoRepository(t *testing.T) {
	const patch = "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"

	m := newModel(Options{Config: config.Default(), Document: diff.Parse(patch)}, "", "")
	// The document is applied when the window size arrives, which is the first
	// moment its notes can be wrapped at the right width.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(model)

	if !m.fromStdin || !m.pinned {
		t.Fatalf("document model: fromStdin=%v pinned=%v, want both", m.fromStdin, m.pinned)
	}
	if paths := listedPaths(m); len(paths) != 1 || paths[0] != "x.go" {
		t.Fatalf("listing = %v, want x.go", paths)
	}
	if m.view.File() == nil {
		t.Fatal("the pane holds no diff")
	}
	// One character per row means the notes were wrapped before the width was
	// known and never wrapped again.
	if rows := m.view.Rows(); rows > 20 {
		t.Fatalf("the document has %d rows, its notes are still one character per row", rows)
	}
	if m.Init() == nil {
		t.Fatal("a document has no commands at all")
	}
}

// Standard input is read once, so a refresh cannot reread it.
func TestDocumentCannotBeReloaded(t *testing.T) {
	const patch = "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"

	m := newModel(Options{Config: config.Default(), Document: diff.Parse(patch)}, "", "")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(model)

	next, _ = m.refresh()
	if got := next.(model).message; got != "standard input is read once" {
		t.Fatalf("refresh said %q", got)
	}
}

// A patch that arrived by mail keeps its message: it is the only explanation of
// why the diff exists, and the pane is where a reader looks for it.
func TestDocumentKeepsTheMailItArrivedIn(t *testing.T) {
	const mail = "From 0123456789abcdef Mon Sep 17 00:00:00 2001\n" +
		"Subject: [PATCH] fix the thing\n" +
		"\n" +
		"the reason it exists\n" +
		"diff --git a/x.go b/x.go\n" +
		"--- a/x.go\n" +
		"+++ b/x.go\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"

	m := newModel(Options{Config: config.Default(), Document: diff.Parse(mail)}, "", "")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = next.(model)

	got := m.render()
	for _, want := range []string{"Subject: [PATCH] fix the thing", "the reason it exists"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the pane lost %q:\n%s", want, got)
		}
	}
}
