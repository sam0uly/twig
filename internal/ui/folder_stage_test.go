package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"samouly.fun/twig/internal/git"
)

// stagingRepo builds a repository with changes in two folders and leaves the
// process inside it, so the git layer resolves the same tree the listing names.
func stagingRepo(t *testing.T) {
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
	write("src/a.go", "package src\n\nvar A = 1\n")
	write("src/deep/b.go", "package deep\n\nvar B = 1\n")
	write("top.txt", "top\n")
	run("add", "-A")
	run("commit", "-qm", "seed")
	write("src/a.go", "package src\n\nvar A = 2\n")
	write("src/deep/b.go", "package deep\n\nvar B = 2\n")
	write("top.txt", "top changed\n")
	t.Chdir(dir)
}

// statusLines returns git's short status, one line per change.
func statusLines(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n")
}

// dirModel opens a tree listing on the repo the test is inside, with the cursor
// on the src folder and the listing taken from the side the caller names.
func dirModel(t *testing.T, staged bool) model {
	t.Helper()
	const dir = "src"
	m := testModel(120, 30)
	var files []git.FileStatus
	for _, line := range statusLines(t) {
		path := strings.TrimSpace(line[2:])
		files = append(files, git.FileStatus{Path: path, Letter: strings.TrimSpace(line[:2])})
	}
	if staged {
		m.files.SetFiles(files, nil)
	} else {
		m.files.SetFiles(nil, files)
	}
	m.files.SetTree(true)
	m.files.SelectDir(dir)
	path, side, ok := m.files.SelectedDir()
	if !ok || path != dir {
		t.Fatalf("the cursor is not on %q: %q", dir, path)
	}
	if side != staged {
		t.Fatalf("folder %q reports side %v, want %v", dir, side, staged)
	}
	m.focus = focusSidebar
	m.syncStaging()
	return m
}

// TestStageKeyStagesEveryFileUnderAFolder is the folder case: the cursor is a
// tree row, and one keypress moves every file inside it to the index, including
// one nested a level deeper and nothing outside the folder.
func TestStageKeyStagesEveryFileUnderAFolder(t *testing.T) {
	stagingRepo(t)
	m := dirModel(t, false)

	next, cmd := m.toggleStage(true)
	if cmd == nil {
		t.Fatalf("s on a folder produced no operation: %q", next.(model).message)
	}
	msg, ok := cmd().(stageDoneMsg)
	if !ok {
		t.Fatal("the command did not produce a stageDoneMsg")
	}
	if msg.err != nil {
		t.Fatalf("staging a folder failed: %v", msg.err)
	}
	if msg.scope != "dir" || msg.files != 2 {
		t.Fatalf("operation was %q covering %d files, want dir and 2", msg.scope, msg.files)
	}

	got := strings.Join(statusLines(t), "\n")
	if !strings.Contains(got, "M  src/a.go") || !strings.Contains(got, "M  src/deep/b.go") {
		t.Fatalf("the folder's files are not staged:\n%s", got)
	}
	if !strings.Contains(got, " M top.txt") {
		t.Fatalf("staging the folder touched a file outside it:\n%s", got)
	}
}

// TestUnstageKeyEmptiesAFolder covers the other direction, one level deeper than
// the folder the cursor is on.
func TestUnstageKeyEmptiesAFolder(t *testing.T) {
	stagingRepo(t)
	if out, err := exec.Command("git", "add", "--", "src").CombinedOutput(); err != nil {
		t.Fatalf("git add src: %v\n%s", err, out)
	}
	m := dirModel(t, true)

	next, cmd := m.toggleStage(false)
	if cmd == nil {
		t.Fatalf("u on a folder produced no operation: %q", next.(model).message)
	}
	if msg, ok := cmd().(stageDoneMsg); !ok || msg.err != nil {
		t.Fatalf("unstage produced %#v", msg)
	}
	for _, line := range statusLines(t) {
		if strings.HasPrefix(line, "A ") || strings.HasPrefix(line, "M ") {
			t.Fatalf("something is still staged:\n%s", strings.Join(statusLines(t), "\n"))
		}
	}
}

// TestFolderKeysSayWhenThereIsNothingToDo: the row already says which side its
// files are on, so asking for that side is a no-op with a reason, not a silent
// keypress or a git error.
func TestFolderKeysSayWhenThereIsNothingToDo(t *testing.T) {
	stagingRepo(t)
	if out, err := exec.Command("git", "add", "--", "src").CombinedOutput(); err != nil {
		t.Fatalf("git add src: %v\n%s", err, out)
	}

	staged := dirModel(t, true)
	next, _ := staged.toggleStage(true)
	if msg := next.(model).message; !strings.Contains(msg, "already staged") {
		t.Fatalf("message %q does not explain why nothing happened", msg)
	}

	if out, err := exec.Command("git", "reset", "-q", "HEAD", "--", "src").CombinedOutput(); err != nil {
		t.Fatalf("git reset src: %v\n%s", err, out)
	}
	unstaged := dirModel(t, false)
	next, _ = unstaged.toggleStage(false)
	if msg := next.(model).message; !strings.Contains(msg, "nothing staged") {
		t.Fatalf("message %q does not explain why nothing happened", msg)
	}
}

// TestFolderScopeDoesNotOpenADiffForTheDirectory: a folder is not a file, so the
// reload must not try to render one, and the reader must not be advanced off the
// row they just acted on.
func TestFolderScopeDoesNotOpenADiffForTheDirectory(t *testing.T) {
	stagingRepo(t)
	m := dirModel(t, false)
	m.lastLoaded.path = "top.txt"

	next, cmd := m.applyStageDone(stageDoneMsg{
		staged: true, op: "stage", path: "src", scope: "dir", files: 2,
	})
	if cmd == nil {
		t.Fatal("no refresh was scheduled")
	}
	got := next.(model)
	if got.advanceList {
		t.Fatal("a folder operation moved the reader on to the next file")
	}
	if got.lastLoaded.path != "top.txt" {
		t.Fatalf("the pane switched to %q, which is not a file", got.lastLoaded.path)
	}
	if !strings.Contains(got.message, "2 files in src") {
		t.Fatalf("message %q does not say what the folder operation did", got.message)
	}
}

// TestFolderRowKnowsItsSide: a folder holding changes on both sides is two rows,
// and each one must mean its own half.
func TestFolderRowKnowsItsSide(t *testing.T) {
	m := testModel(100, 24)
	files := []git.FileStatus{
		{Path: "src/a.go", Letter: "M"},
		{Path: "src/b.go", Letter: "M"},
	}
	m.files.SetFiles(files, files)
	m.files.SetTree(true)

	var seen []bool
	m.files.Select(0)
	for {
		if path, side, ok := m.files.SelectedDir(); ok && path == "src" {
			seen = append(seen, side)
			if got := len(m.files.FilesUnder("src", side)); got != 2 {
				t.Fatalf("%d files under src on side %v, want 2", got, side)
			}
		}
		if !m.files.MoveSelectable(1) {
			break
		}
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("folder rows reported sides %v, want one row per side", seen)
	}
}
