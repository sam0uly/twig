package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"twig/internal/diff"
	"twig/internal/git"
	"twig/internal/ui/components/filelist"
)

// TestCommitLoadKeepsFileDiffArriving pins the sequencing rule that made a large
// file open to an empty pane: a commit load must not discard a file diff that is
// still on its way, because the two are different selections.
func TestCommitLoadKeepsFileDiffArriving(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)

	// A file diff is in flight.
	next, _ := m.showFile("a.go", false)
	m = next.(model)
	fileSeq := m.seq

	// A commit load starts and finishes in the meantime.
	next, _ = m.showCommit(git.Commit{Hash: "h", Short: "abc1234"})
	m = next.(model)
	if m.commitSeq == 0 {
		t.Fatal("the commit load did not take a sequence number")
	}

	// The file diff finally arrives; it must still be shown.
	f := diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	next, _ = m.Update(fileLoadedMsg{file: f, path: "a.go", seq: fileSeq})
	m = next.(model)
	if m.view.Empty() {
		t.Fatal("the file diff was dropped by a commit load")
	}
	if m.view.File() != f {
		t.Fatal("the pane does not show the file diff")
	}
}

// TestCommitLoadDropsStaleCommit checks that the commit counter still drops a
// commit the reader has already moved past.
func TestCommitLoadDropsStaleCommit(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(filelist.TabCommits)

	next, _ := m.showCommit(git.Commit{Hash: "first", Short: "aaaaaaa"})
	m = next.(model)
	stale := m.commitSeq
	next, _ = m.showCommit(git.Commit{Hash: "second", Short: "bbbbbbb"})
	m = next.(model)

	f := diff.Generate("x.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	next, _ = m.Update(commitLoadedMsg{commit: git.Commit{Hash: "first", Short: "aaaaaaa"}, file: f, files: []*diff.File{f}, seq: stale})
	m = next.(model)
	if !m.view.Empty() {
		t.Fatal("a stale commit load repainted the pane")
	}
}

// TestLogLoadLeavesFilesTabAlone pins that loading the log does not open a
// commit while the Files tab owns the pane.
func TestLogLoadLeavesFilesTabAlone(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(filelist.TabFiles)

	next, cmd := m.Update(logLoadedMsg{commits: []git.Commit{{Hash: "h", Short: "abc1234", Subject: "s"}}})
	m = next.(model)
	if cmd != nil {
		if _, ok := cmd().(commitListMsg); ok {
			t.Fatal("loading the log started a commit load on the Files tab")
		}
	}
	if m.commitSeq != 0 {
		t.Fatal("loading the log took a commit sequence number on the Files tab")
	}
	if !m.view.Empty() {
		t.Fatal("loading the log painted the pane on the Files tab")
	}
}

// TestCommitListLoadsFirstForCommitList checks that selecting a commit asks for
// the tree-only listing first, and that the eager or lazy decision follows.
func TestCommitListLoadsFirstForCommitList(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(filelist.TabCommits)

	next, cmd := m.showCommit(git.Commit{Hash: "h", Short: "abc1234"})
	m = next.(model)
	if cmd == nil {
		t.Fatal("selecting a commit should start a load")
	}
	if _, ok := cmd().(commitListMsg); !ok {
		t.Fatalf("first load = %T, want commitListMsg", cmd())
	}

	// A small commit goes on to the eager aggregate.
	small := []git.CommitChange{{Path: "a.go", Kind: diff.Modified}}
	next, cmd = m.Update(commitListMsg{commit: m.commit, changes: small, seq: m.commitSeq})
	m = next.(model)
	if cmd == nil {
		t.Fatal("a small commit should load its diff")
	}
	if _, ok := cmd().(commitLoadedMsg); !ok {
		t.Fatalf("small commit load = %T, want commitLoadedMsg", cmd())
	}

	// A large one switches to the streaming document.
	large := make([]git.CommitChange, commitEagerFiles+1)
	for i := range large {
		large[i] = git.CommitChange{Path: "file" + string(rune('a'+i%26)) + ".go", Kind: diff.Modified}
	}
	next, cmd = m.Update(commitListMsg{commit: m.commit, changes: large, seq: m.commitSeq})
	m = next.(model)
	if cmd == nil {
		t.Fatal("a large commit should start streaming its files")
	}
	if _, ok := cmd().(commitChunkMsg); !ok {
		t.Fatalf("large commit load = %T, want commitChunkMsg", cmd())
	}
	if len(m.commitFileList) != len(large) {
		t.Fatalf("sidebar has %d files, want %d", len(m.commitFileList), len(large))
	}
}

// TestCommitChunkFillsSidebarStats checks that the streaming document fills in
// the counts it could not know from tree data.
func TestCommitChunkFillsSidebarStats(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(filelist.TabCommits)
	// A commit is listed, so the welcome screen does not own the pane.
	m.files.SetCommits([]git.Commit{{Hash: "h", Short: "abc1234", Subject: "s"}})
	m.setSize()
	m.commitSeq = 3

	changes := []git.CommitChange{
		{Path: "a.go", Kind: diff.Modified},
		{Path: "b.go", Kind: diff.Modified},
	}
	m.applyCommitLazy(git.Commit{Hash: "h", Short: "abc1234"}, changes)
	for i, f := range m.commitFileList {
		if f.Additions >= 0 || f.Deletions >= 0 {
			t.Fatalf("file %d starts with known counts", i)
		}
	}

	files := []*diff.File{
		{NewPath: "a.go", Additions: 4, Deletions: 2},
		{NewPath: "b.go", Additions: 1, Deletions: 1},
	}
	next, _ := m.Update(commitChunkMsg{changes: changes, files: files, from: 0, seq: m.commitSeq})
	m = next.(model)

	if m.commitFileList[0].Additions != 4 || m.commitFileList[1].Deletions != 1 {
		t.Fatalf("sidebar stats not filled: %+v", m.commitFileList)
	}
	if m.d == nil || m.d.Additions != 5 || m.d.Deletions != 3 {
		t.Fatalf("totals = %+v, want +5 -3", m.d)
	}
	if m.view.Empty() {
		t.Fatal("the streaming document has no rows")
	}
	if !strings.Contains(ansi.Strip(m.render()), "a.go") {
		t.Fatalf("the document does not render the first file:\n%s", ansi.Strip(m.render()))
	}
}

// TestFileLoadFailureReplacesPane checks that a failed load says so where the
// diff would be, instead of leaving an unrelated document on screen.
func TestFileLoadFailureReplacesPane(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	m.view.SetFile(diff.Generate("stale.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))

	next, _ := m.showFile("a.go", false)
	m = next.(model)
	next, _ = m.Update(fileLoadedMsg{path: "a.go", err: errors.New("read failed"), seq: m.seq})
	m = next.(model)

	if m.view.Empty() {
		t.Fatal("the pane went blank instead of explaining the failure")
	}
	f := m.view.File()
	if f == nil || f.NLines() != 1 {
		t.Fatalf("pane holds %+v, want a one-line notice", f)
	}
	if got := f.LineText(0); !strings.Contains(got, "diff unavailable") || !strings.Contains(got, "read failed") {
		t.Fatalf("notice = %q", got)
	}
	if m.d != nil {
		t.Fatal("totals were left over from the previous file")
	}
}

// TestCommitLoadFailureReplacesPane does the same for a commit.
func TestCommitLoadFailureReplacesPane(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(filelist.TabCommits)
	m.view.SetFile(diff.Generate("stale.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))

	next, _ := m.showCommit(git.Commit{Hash: "h", Short: "abc1234"})
	m = next.(model)
	next, _ = m.Update(commitListMsg{commit: m.commit, err: errors.New("object not found"), seq: m.commitSeq})
	m = next.(model)

	f := m.view.File()
	if f == nil {
		t.Fatal("the pane kept no document at all")
	}
	if got := f.LineText(0); !strings.Contains(got, "diff unavailable") || !strings.Contains(got, "object not found") {
		t.Fatalf("notice = %q", got)
	}
}
