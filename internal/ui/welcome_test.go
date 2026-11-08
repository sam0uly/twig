package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/filelist"
)

func TestWelcomeVisibility(t *testing.T) {
	tests := []struct {
		name      string
		loaded    bool
		logLoaded bool
		tab       int
		files     []git.FileStatus
		commits   []git.Commit
		want      bool
	}{
		{name: "status loading", want: false},
		{name: "clean files", loaded: true, want: true},
		{name: "changed files", loaded: true, files: []git.FileStatus{{Path: "a.go"}}, want: false},
		{name: "commits loading", loaded: true, tab: filelist.TabCommits, want: false},
		{name: "no commits", loaded: true, logLoaded: true, tab: filelist.TabCommits, want: true},
		{name: "a commit", loaded: true, logLoaded: true, tab: filelist.TabCommits, commits: []git.Commit{{Hash: "abc"}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(90, 24)
			m.loaded = tt.loaded
			m.logLoaded = tt.logLoaded
			m.files.SetTab(tt.tab)
			m.files.SetFiles(nil, tt.files)
			m.files.SetCommits(tt.commits)
			if got := m.showWelcome(); got != tt.want {
				t.Fatalf("showWelcome() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWelcomeUsesPlaygroundKeysWithoutChangingTheRepository(t *testing.T) {
	m := testModel(100, 30)
	beforeCfg := m.cfg
	beforeFiles := m.files.Total()
	before := m.welcomeView()

	next, cmd := m.Update(keyPress("c"))
	got := next.(model)
	if cmd != nil {
		t.Fatal("a playground palette change should not run a repository command")
	}
	if got.cfg != beforeCfg || got.files.Total() != beforeFiles || got.state != stateShowList {
		t.Fatal("a playground edit escaped into repository state")
	}
	if got.welcomeView() == before {
		t.Fatal("c did not change the playground's leaf palette")
	}
	if m.playground.palette != 0 || m.playground.tick != 0 || m.playground.sprouts != 0 {
		t.Fatal("updating the returned model mutated the previous model's bonsai")
	}

	for _, key := range []string{"C", "P", "s", "u", "H", "L", "Z", "V"} {
		next, cmd = got.Update(keyPress(key))
		got = next.(model)
		if cmd != nil {
			t.Fatalf("%q ran a repository command while idle", key)
		}
		if got.cfg != beforeCfg || got.files.Total() != beforeFiles || got.state != stateShowList {
			t.Fatalf("%q escaped the playground", key)
		}
	}
}

func TestWelcomeAllBonsaiControlsStayInMemory(t *testing.T) {
	m := testModel(100, 30)
	beforeCfg := m.cfg
	beforeFiles := m.files.Total()
	for _, key := range []string{"h", "l", "b", "b", "c", "m", " ", "z", "x"} {
		next, cmd := m.Update(keyPress(key))
		m = next.(model)
		if cmd != nil {
			t.Fatalf("%q launched a repository command while tending pixel art", key)
		}
		if m.cfg != beforeCfg || m.files.Total() != beforeFiles || m.state != stateShowList {
			t.Fatalf("%q changed repository-facing state", key)
		}
	}
}

func TestWelcomeTickAdvancesPrivateSpringState(t *testing.T) {
	m := testModel(100, 30)
	next, cmd := m.Update(keyPress("b"))
	m = next.(model)
	if cmd != nil || m.playground.sprouts != 1 {
		t.Fatal("failed to start a bonsai sprout")
	}
	beforeTick := m
	next, cmd = m.Update(welcomeTickMsg{})
	got := next.(model)
	if cmd == nil {
		t.Fatal("visible spring tick should schedule the next tick")
	}
	if got.playground.tick != 1 || got.playground.growth[0].position == 0 || got.playground.trunk == beforeTick.playground.trunk {
		t.Fatal("welcomeTickMsg did not integrate the bonsai springs")
	}
	if beforeTick.playground.tick != 0 || beforeTick.playground.growth[0].position != 0 {
		t.Fatal("ticking the returned model changed the previous model's springs")
	}
}

func TestEmptyCommitsHideBothSidebarsWithoutForgettingPreferences(t *testing.T) {
	m := testModel(110, 28)
	m.showLeft, m.showRight = true, true
	m.cfg.ShowLeftSidebar, m.cfg.ShowRightSidebar = true, true
	next, _ := m.Update(logLoadedMsg{})
	m = next.(model)
	m.files.SetTab(filelist.TabCommits)
	m.focus = focusMain
	m.setSize()

	if !m.showWelcome() {
		t.Fatal("loaded empty commits should show the playground")
	}
	if m.sideRect.Dx() != 0 || m.commitFilesRct.Dx() != 0 {
		t.Fatalf("sidebars still occupy space: left=%d right=%d", m.sideRect.Dx(), m.commitFilesRct.Dx())
	}
	if m.mainRect.Dx() != m.width {
		t.Fatalf("main width = %d, want %d", m.mainRect.Dx(), m.width)
	}
	if !m.showLeft || !m.showRight || !m.cfg.ShowLeftSidebar || !m.cfg.ShowRightSidebar {
		t.Fatal("empty commits overwrote sidebar preferences")
	}
	assertGeometry(t, m, "empty commits")

	next, _ = m.Update(logLoadedMsg{commits: []git.Commit{{Hash: "abc", Short: "abc", Subject: "one"}}})
	m = next.(model)
	if m.sideRect.Dx() == 0 || m.commitFilesRct.Dx() == 0 {
		t.Fatal("sidebars did not return when commits arrived")
	}
}

func TestEmptyCommitStartupOrderKeepsSidebarsHidden(t *testing.T) {
	m := testModel(110, 28)
	m.loaded = false
	m.showLeft, m.showRight = true, true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()

	next, _ := m.Update(logLoadedMsg{})
	m = next.(model)
	if m.showWelcome() {
		t.Fatal("an empty log appeared before the initial status load")
	}
	next, _ = m.Update(statusLoadedMsg{})
	m = next.(model)
	if !m.showWelcome() || m.sideRect.Dx() != 0 || m.commitFilesRct.Dx() != 0 || m.mainRect.Dx() != m.width {
		t.Fatalf("late status left stale geometry: welcome=%v left=%d right=%d main=%d",
			m.showWelcome(), m.sideRect.Dx(), m.commitFilesRct.Dx(), m.mainRect.Dx())
	}
}

func TestEmptyCommitLoadClearsStaleDiff(t *testing.T) {
	m := testModel(100, 30)
	m.showRight = true
	m.files.SetTab(filelist.TabCommits)
	m.files.SetCommits([]git.Commit{{Hash: "abc", Short: "abc", Subject: "old"}})
	file := diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	m.applyCommit(git.Commit{Hash: "abc", Short: "abc", Subject: "old"}, file, []*diff.File{file})
	m.commitSeq = 4
	m.search.Activate()
	m.searchApplied = true

	next, _ := m.Update(logLoadedMsg{})
	got := next.(model)
	if !got.showWelcome() || !got.view.Empty() || got.d != nil || got.commitFileList != nil {
		t.Fatal("empty commits kept stale commit content behind the playground")
	}
	if got.focus != focusMain || got.search.Active() || got.searchApplied {
		t.Fatal("hidden content kept focus or search input instead of the playground")
	}
	if got.commitSeq == m.commitSeq {
		t.Fatal("empty commits did not invalidate the old commit load")
	}
	next, _ = got.Update(commitLoadedMsg{
		commit: git.Commit{Hash: "abc", Short: "abc", Subject: "old"},
		file:   file,
		files:  []*diff.File{file},
		seq:    m.commitSeq,
	})
	got = next.(model)
	if !got.view.Empty() || !got.playgroundInput() {
		t.Fatal("an obsolete commit load refilled the empty playground")
	}
	next, cmd := got.Update(keyPress("b"))
	got = next.(model)
	if cmd != nil || got.playground.sprouts != 1 {
		t.Fatal("playground keys did not work after stale content was cleared")
	}
}

func TestSwitchToLoadedEmptyCommitsClearsFileDiff(t *testing.T) {
	m := testModel(100, 30)
	m.logLoaded = true
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go"}})
	file := diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	m.view.SetFile(file)
	m.d = &diff.Diff{Additions: 1, Deletions: 1}

	next, _ := m.Update(keyPress("2"))
	got := next.(model)
	if !got.showWelcome() || !got.playgroundInput() || !got.view.Empty() || got.d != nil {
		t.Fatal("empty commits kept a file diff behind the playground")
	}
	next, cmd := got.Update(keyPress("b"))
	got = next.(model)
	if cmd != nil || got.playground.sprouts != 1 {
		t.Fatal("the empty commits playground did not accept its sprout key")
	}
}

func TestCleanStatusRejectsAnInFlightDiff(t *testing.T) {
	m := testModel(100, 30)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go"}})
	m.seq = 5

	next, _ := m.Update(statusLoadedMsg{})
	got := next.(model)
	if !got.playgroundInput() || got.focus != focusMain {
		t.Fatal("clean status did not transfer input to the playground")
	}
	file := diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	next, _ = got.Update(fileLoadedMsg{path: "a.go", file: file, seq: m.seq})
	got = next.(model)
	if !got.view.Empty() || !got.playgroundInput() {
		t.Fatal("an obsolete file load refilled the clean playground")
	}
}

func TestWelcomeKeepsGlobalKeys(t *testing.T) {
	m := testModel(100, 30)
	next, _ := m.Update(keyPress("?"))
	if !next.(model).help {
		t.Fatal("help did not open over the playground")
	}

	m = testModel(100, 30)
	m.logLoaded = true
	next, _ = m.Update(keyPress("2"))
	m = next.(model)
	if m.files.Tab() != filelist.TabCommits || !m.playgroundInput() {
		t.Fatal("commits tab did not open its empty playground")
	}
	next, _ = m.Update(keyPress("1"))
	if next.(model).files.Tab() != filelist.TabFiles {
		t.Fatal("files tab did not reopen from the playground")
	}

	m = testModel(100, 30)
	if _, cmd := m.Update(keyPress("r")); cmd == nil {
		t.Fatal("refresh was swallowed by the playground")
	}
	if _, cmd := m.Update(keyPress("q")); cmd == nil {
		t.Fatal("quit was swallowed by the playground")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T", cmd())
	}
}

func TestWelcomeMouseDoesNotSelectStaleDiff(t *testing.T) {
	m := testModel(100, 30)
	m.view.StartReadSelection(0, 0)
	next, _ := m.updateClick(tea.MouseClickMsg{X: 50, Y: 10, Button: tea.MouseLeft})
	if next.(model).view.ReadSelecting() {
		t.Fatal("welcome click started or kept a diff selection")
	}
}

func TestWelcomeStatusNamesTheEmptyTab(t *testing.T) {
	m := testModel(100, 30)
	for _, tt := range []struct {
		tab  string
		want string
	}{
		{tab: "files", want: "working tree clean · main"},
		{tab: "commits", want: "no commits yet · main"},
	} {
		t.Run(tt.tab, func(t *testing.T) {
			if tt.tab == "commits" {
				m.files.SetTab(filelist.TabCommits)
				m.logLoaded = true
			}
			if got := m.welcomeView(); !strings.Contains(got, tt.want) {
				t.Fatalf("welcome missing %q", tt.want)
			}
		})
	}
}
