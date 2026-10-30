package ui

import (
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/tone"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/diffview"
	"samouly.fun/twig/internal/ui/components/filelist"
	"samouly.fun/twig/internal/ui/styles"
)

func testModel(width, height int) model {
	m := newModel(Options{Config: config.Default(), Source: git.Source{}}, "main", "twig")
	m.width, m.height = width, height
	m.loaded = true
	m.setSize()
	return m
}

func assertGeometry(t *testing.T, m model, label string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(m.render(), "\n"), "\n")
	if len(lines) != m.height {
		t.Fatalf("%s: rendered %d lines, want %d", label, len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != m.width {
			t.Fatalf("%s: line %d width = %d, want %d", label, i, w, m.width)
		}
	}
}

func TestLayoutFilesTab(t *testing.T) {
	m := testModel(120, 24)
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "package a\n", "package b\n", diff.GenerateOptions{Context: 3}))
	m.files.SetFiles(
		[]git.FileStatus{{Path: "internal/ui/tui.go", Letter: "M"}},
		[]git.FileStatus{{Path: "internal/ui/components/filelist/filelist.go", Letter: "M"}},
	)
	assertGeometry(t, m, "files")
}

func TestLayoutCommitsTab(t *testing.T) {
	m := testModel(120, 24)
	m.showRight = true
	m.files.SetTab(1)
	m.setSize()

	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234", Subject: "subject", Author: "sam", When: "now"},
		diff.Aggregate(files), files)
	m.files.SetCommits([]git.Commit{{Hash: "h", Short: "abc1234", Subject: "subject"}})

	assertGeometry(t, m, "commits")
}

// TestListViewsFitPane guards the sidebar clipping regression: a list view
// must never be taller than the pane body it is rendered into.
func TestListViewsFitPane(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(1)
	m.setSize()

	m.files.SetCommits([]git.Commit{
		{Hash: "1", Short: "a", Subject: "one"},
		{Hash: "2", Short: "b", Subject: "two"},
	})
	m.files.SetCommitFiles([]*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{}),
	})

	if got, want := len(strings.Split(m.files.View(), "\n")), m.sideRect.Dy()-2; got > want {
		t.Fatalf("commits list rendered %d lines into a %d line pane", got, want)
	}
	if got, want := len(strings.Split(m.files.CommitFilesView(), "\n")), m.commitFilesRct.Dy()-2; got > want {
		t.Fatalf("commit files list rendered %d lines into a %d line pane", got, want)
	}
}

func TestMainPaneShowsLineNumbers(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"package a\n\nfunc x() {}\n", "package a\n\nfunc y() {}\n", diff.GenerateOptions{Context: 3}))
	if !strings.Contains(m.render(), "   1    1") {
		t.Fatalf("expected line numbers in the main pane:\n%s", m.render())
	}
}

func TestSplitViewToggle(t *testing.T) {
	m := testModel(120, 24)
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "old line\n", "new line\n", diff.GenerateOptions{Context: 3}))

	unified := m.view.View()
	m.view.ToggleSplit()
	if !m.view.Split() {
		t.Fatal("split mode not enabled")
	}
	split := m.view.View()
	if split == unified {
		t.Fatal("split view did not change the rendering")
	}
	assertGeometry(t, m, "split")
}

func TestHeaderGradientCoversProjectName(t *testing.T) {
	m := testModel(120, 24)
	header := strings.Split(m.render(), "\n")[0]
	if !strings.Contains(ansi.Strip(header), "Twig · twig") {
		t.Fatalf("project name missing from header: %q", ansi.Strip(header))
	}
	// The whole logo + dot + project name run must be gradient-coloured: the
	// plain " Twig · twig " text is 13 cells, each with its own foreground.
	if got, want := strings.Count(header, "38;2;"), 13; got < want {
		t.Fatalf("gradient foreground applied %d times, want at least %d", got, want)
	}
}

func TestShowFileUsesCache(t *testing.T) {
	m := testModel(120, 24)
	f := diff.Generate("cached.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	m.cache.Put(fileCacheKey("cached.go", false, m.cfg), cacheEntry{file: f})

	next, cmd := m.showFile("cached.go", false)
	if cmd != nil {
		t.Fatal("cache hit should not schedule a load")
	}
	if got := next.(model).view.File(); got != f {
		t.Fatal("cached diff was not displayed")
	}
}

func TestShowFileLoadsImmediately(t *testing.T) {
	m := testModel(120, 24)

	next, cmd := m.showFile("first.go", false)
	if cmd == nil {
		t.Fatal("cache miss should issue a load right away")
	}
	first := next.(model)

	next, _ = first.showFile("second.go", false)
	second := next.(model)
	if second.seq != first.seq+1 {
		t.Fatalf("seq = %d, want %d", second.seq, first.seq+1)
	}

	// A result whose seq is no longer current must not replace the view.
	f := diff.Generate("stale.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3})
	m.cache.Put(fileCacheKey("stale.go", false, m.cfg), cacheEntry{file: f})
	nm, _ := second.Update(fileLoadedMsg{file: f, path: "stale.go", staged: false, seq: first.seq})
	if nm.(model).view.File() == f {
		t.Fatal("stale load replaced the current diff")
	}
	if _, ok := second.cache.Get(fileCacheKey("stale.go", false, m.cfg)); !ok {
		t.Fatal("prefetched/stale diffs should still be cached")
	}
}

func TestNeighbourPrefetchCachesDiffs(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	m.files.SelectPath("b.go", false)

	next, cmd := m.prefetchNeighbors()
	if cmd == nil {
		t.Fatal("expected prefetch commands for neighbouring files")
	}
	// The prefetch cursor is tracked so repeating the same selection is free.
	if _, again := next.prefetchNeighbors(); again != nil {
		t.Fatal("prefetch should not repeat for the same cursor")
	}
}

func TestStatusReloadClearsDiffCache(t *testing.T) {
	m := testModel(120, 24)
	m.cache.Put("k", cacheEntry{file: &diff.File{}})

	next, _ := m.Update(statusLoadedMsg{})
	if _, ok := next.(model).cache.Get("k"); ok {
		t.Fatal("stale diffs survived a working tree refresh")
	}
}

func TestLayoutWithHelp(t *testing.T) {
	for _, commits := range []bool{false, true} {
		m := testModel(120, 30)
		if commits {
			m.files.SetTab(1)
		}
		m.help = true
		m.setSize()
		m.view.SetFile(diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))
		assertGeometry(t, m, "help")
	}
}

func TestArrowKeysScrollTheDiff(t *testing.T) {
	m := testModel(60, 20)
	f := diff.Generate("long.go", diff.Modified,
		strings.Repeat("wide line of text that overflows the pane\n", 3),
		strings.Repeat("wide line of text that overflows the pane!\n", 3),
		diff.GenerateOptions{Context: 3})
	m.showLeft = true
	m.showRight = true
	m.setSize()
	m.view.SetFile(f)
	m.focus = focusMain

	left := tea.KeyPressMsg{Code: tea.KeyLeft}
	right := tea.KeyPressMsg{Code: tea.KeyRight}

	m.view, _ = m.view.Update(right)
	if x := m.view.XOffset(); x == 0 {
		t.Fatal("right arrow did not scroll the diff horizontally")
	}

	before := m.view.XOffset()
	next, _ := m.Update(left)
	m = next.(model)
	if m.focus != focusMain {
		t.Fatal("left arrow switched the focused pane instead of scrolling")
	}
	if x := m.view.XOffset(); x >= before {
		t.Fatalf("left arrow did not scroll back (x = %d, was %d)", x, before)
	}
}

func TestSplitKeyBinding(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	if !key.Matches(tea.KeyPressMsg{Code: 'v', Text: "v"}, m.keys.ToggleView) {
		t.Fatal("v is not bound to the diff view toggle")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !next.(model).view.Split() {
		t.Fatal("v did not enable split view")
	}
}

func TestWelcomeGeometry(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {40, 12}} {
		m := testModel(size[0], size[1])
		if !m.showWelcome() {
			t.Fatalf("%dx%d: welcome should be visible with no changes", size[0], size[1])
		}
		assertGeometry(t, m, "welcome")
	}
}

func TestWelcomeAnimationFrames(t *testing.T) {
	m := testModel(100, 30)
	first := m.welcomeView()

	next, cmd := m.Update(welcomeTickMsg{})
	if cmd == nil {
		t.Fatal("welcome tick should schedule the next frame")
	}
	m = next.(model)
	if m.welcomeFrame != 1 {
		t.Fatalf("welcome frame = %d, want 1", m.welcomeFrame)
	}
	if m.welcomeView() == first {
		t.Fatal("animation frame did not change the welcome art")
	}

	// The animation stops as soon as there is something to browse.
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	nm, cmd := m.Update(welcomeTickMsg{})
	if cmd != nil {
		t.Fatal("welcome tick should stop when the file list has entries")
	}
	if nm.(model).welcomeFrame != m.welcomeFrame {
		t.Fatal("welcome frame advanced while hidden")
	}
}

func TestWelcomeContent(t *testing.T) {
	m := testModel(100, 30)
	view := ansi.Strip(m.welcomeView())
	t.Logf("welcome screen:\n%s", view)

	for _, want := range []string{"twig", "working tree clean", "git log", "●", "files", "commits"} {
		if !strings.Contains(view, want) {
			t.Fatalf("welcome screen missing %q:\n%s", want, view)
		}
	}

	m.welcomeFrame = welcomeRevealFrames + 5
	t.Logf("scrolled graph:\n%s", ansi.Strip(m.welcomeView()))
}

func TestCommitModalOverDimmedPage(t *testing.T) {
	m := testModel(100, 30)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))
	m.state = stateCompose

	view := m.composeModal()
	t.Logf("commit modal:\n%s", ansi.Strip(view))

	for _, want := range []string{"Commit 1 staged file", "ctrl+s", "esc", "╭", "M a.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("commit modal missing %q:\n%s", want, ansi.Strip(view))
		}
	}
	// The page behind is dimmed cell by cell: the bright green stat color of
	// the header must be scaled down, not left untouched.
	if strings.Contains(view, "0;255;178") {
		t.Fatal("background page was not dimmed")
	}
	// Plain text has no explicit color, and used to stay at full brightness
	// (the header fill, the line-number gutter). It must be dimmed to the
	// scaled-down body color.
	if !strings.Contains(view, "38;2;96;94;99") {
		t.Fatal("unstyled page text (header fill, line numbers) was not dimmed")
	}
	// The editor is a lifted Char panel: the background must cover its rows,
	// including the padding, so nothing behind shows through.
	if !strings.Contains(view, "48;2;58;57;67") {
		t.Fatal("dialog editor background is missing")
	}
	// The dialog keeps its own bright border.
	if !strings.Contains(view, "38;2;139;117;255") {
		t.Fatal("dialog border color missing")
	}
}

func TestComposeDialogTitleAndHintsInBorder(t *testing.T) {
	m := testModel(100, 30)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	m.state = stateCompose

	lines := strings.Split(ansi.Strip(m.composeDialog()), "\n")
	if len(lines) < 3 {
		t.Fatalf("dialog is too short: %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "╭─ Commit 1 staged file") {
		t.Fatalf("title is not embedded in the top border: %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰─ ctrl+s") {
		t.Fatalf("hints are not embedded in the bottom border: %q", lines[len(lines)-1])
	}
}

func TestComposeInputIsFilledWithPanelColor(t *testing.T) {
	m := testModel(100, 30)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	m.state = stateCompose

	dialog := m.composeDialog()

	// No left prompt bar inside the editor.
	if strings.Contains(ansi.Strip(dialog), "┃") {
		t.Fatalf("editor still draws a left border/prompt:\n%s", ansi.Strip(dialog))
	}
}

// TestPaintCanvasRectFillsResets guards the empty-editor background: the
// textarea emits resets mid-line, which used to leave transparent cells.
func TestPaintCanvasRectFillsResets(t *testing.T) {
	c := lipgloss.NewCanvas(12, 2)
	lipgloss.NewLayer("ab\x1b[0mcd").Draw(c, uv.Rect(0, 0, 12, 2))
	paintCanvasRect(c, uv.Rect(0, 0, 12, 2), tone.Char)

	for y := 0; y < 2; y++ {
		for x := 0; x < 12; x++ {
			cell := c.CellAt(x, y)
			if cell == nil || cell.Style.Bg == nil {
				t.Fatalf("cell (%d,%d) is still transparent", x, y)
			}
		}
	}
}

func TestComposeInputScrollbar(t *testing.T) {
	m := testModel(100, 30)
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	m.state = stateCompose

	m.compose.SetValue("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	dialog := ansi.Strip(m.composeDialog())

	if !strings.Contains(dialog, "┃") {
		t.Fatalf("editor scrollbar thumb missing:\n%s", dialog)
	}
	if !strings.Contains(dialog, "│") {
		t.Fatalf("editor scrollbar track missing:\n%s", dialog)
	}
}

func TestComposeDialogGeometry(t *testing.T) {
	for _, w := range []int{50, 100, 190} {
		m := testModel(w, 40)
		m.files.SetFiles([]git.FileStatus{
			{Path: "a.go", Letter: "M"},
			{Path: "b.go", Letter: "A"},
			{Path: "c.go", Letter: "D"},
			{Path: "d.go", Letter: "M"},
			{Path: "e.go", Letter: "M"},
			{Path: "f.go", Letter: "M"},
		}, nil)
		m.state = stateCompose

		// Every dialog row starts with the accent bar and must have the same
		// width, and the dialog must fit the terminal.
		rows := 0
		var rowW int
		for _, line := range strings.Split(ansi.Strip(m.composeDialog()), "\n") {
			if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
				continue
			}
			rows++
			if rowW == 0 {
				rowW = lipgloss.Width(line)
				continue
			}
			if got := lipgloss.Width(line); got != rowW {
				t.Fatalf("w=%d: dialog row is %d wide, want %d", w, got, rowW)
			}
		}
		if rows < 6 {
			t.Fatalf("w=%d: only %d dialog rows found", w, rows)
		}
		if rowW > w {
			t.Fatalf("w=%d: dialog is %d wide", w, rowW)
		}
		if w == 100 {
			t.Logf("dialog with many staged files:\n%s", ansi.Strip(m.composeDialog()))
		}
	}
}

func TestSidebarToggles(t *testing.T) {
	m := testModel(120, 24)
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})

	// Both sidebars start hidden; the toggles bring them in and out.
	if m.showLeft || m.sideRect.Dx() != 0 {
		t.Fatalf("left sidebar starts visible (width %d)", m.sideRect.Dx())
	}

	left := tea.KeyPressMsg{Code: 'H', Text: "H"}
	next, _ := m.Update(left)
	m = next.(model)
	if !m.showLeft || m.sideRect.Dx() == 0 {
		t.Fatal("H did not show the left sidebar")
	}
	assertGeometry(t, m, "left shown")

	next, _ = m.Update(left)
	m = next.(model)
	if m.showLeft || m.sideRect.Dx() != 0 {
		t.Fatalf("second H did not hide the left sidebar (width %d)", m.sideRect.Dx())
	}
	assertGeometry(t, m, "left hidden")

	m.files.SetTab(1)
	m.setSize()
	if m.showRight || m.commitFilesRct.Dx() != 0 {
		t.Fatalf("right sidebar starts visible (width %d)", m.commitFilesRct.Dx())
	}
	right := tea.KeyPressMsg{Code: 'L', Text: "L"}
	next, _ = m.Update(right)
	m = next.(model)
	if m.commitFilesRct.Dx() == 0 {
		t.Fatal("L did not show the right sidebar")
	}
	assertGeometry(t, m, "right shown")

	next, _ = m.Update(right)
	m = next.(model)
	if m.commitFilesRct.Dx() != 0 {
		t.Fatalf("second L did not hide the right sidebar (width %d)", m.commitFilesRct.Dx())
	}
	assertGeometry(t, m, "right hidden")
}

func TestTreeKeyBinding(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "internal/ui/tui.go", Letter: "M"}})

	if !key.Matches(tea.KeyPressMsg{Code: 't', Text: "t"}, m.keys.ToggleTree) {
		t.Fatal("t is not bound to the tree toggle")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if !next.(model).files.Tree() {
		t.Fatal("t did not enable tree mode")
	}
}

func TestHelpHasNoBackgroundBar(t *testing.T) {
	m := testModel(120, 30)
	m.help = true
	m.setSize()

	help, _ := m.helpView()
	if strings.Contains(help, "48;2;") {
		t.Fatalf("help panel should be plain text, found a background color:\n%s", help)
	}
}

func TestClickCommitFileSelectsAndJumps(t *testing.T) {
	m := testModel(120, 24)
	m.showRight = true
	m.files.SetTab(1)
	m.setSize()

	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("c.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	if m.commitFilesRct.Dx() == 0 {
		t.Fatal("commit files pane has no width")
	}

	// Click the second row of the per-commit sidebar body.
	next, _ := m.Update(tea.MouseClickMsg{X: m.commitFilesRct.Min.X + 2, Y: m.commitFilesRct.Min.Y + 2})
	m = next.(model)

	if got := m.files.CommitFilesIndex(); got != 1 {
		t.Fatalf("clicked row selected index %d, want 1", got)
	}
	if m.focus != focusCommitFiles {
		t.Fatal("click did not focus the commit sidebar")
	}
}

func TestClickFileSelectsInSidebar(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	m.showLeft = true
	m.setSize()

	// Row 0 is the section header, row 1 the first file, row 2 the second.
	next, _ := m.Update(tea.MouseClickMsg{X: m.sideRect.Min.X + 2, Y: m.sideRect.Min.Y + 3})
	m = next.(model)

	if f, _ := m.files.SelectedFile(); f.Path != "b.go" {
		t.Fatalf("click selected %q, want b.go", f.Path)
	}
}

func TestNextPrevFileKeysStepAndLoad(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	m.focus = focusSidebar

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'j', ShiftedCode: 'J', Text: "J"})
	m = next.(model)
	if f, _ := m.files.SelectedFile(); f.Path != "b.go" {
		t.Fatalf("J selected %q, want b.go", f.Path)
	}
	if cmd == nil {
		t.Fatal("J should issue a diff load")
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 'k', ShiftedCode: 'K', Text: "K"})
	m = next.(model)
	if f, _ := m.files.SelectedFile(); f.Path != "a.go" {
		t.Fatalf("K selected %q, want a.go", f.Path)
	}
}

func TestHunkKeysWorkFromTheSidebar(t *testing.T) {
	m := testModel(100, 20)
	// Hunk navigation scrolls immediately in this test, and the view is
	// short enough that there is somewhere to scroll to.
	m.view = diffview.New(false, m.styles)
	m.view.SetSize(80, 8)
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n",
		"a\nB\nc\nd\ne\nf\ng\nH\ni\nj\nk\nl\nm\nn\no\np\n",
		diff.GenerateOptions{Context: 1}))
	m.focus = focusSidebar

	// First press lands on the first hunk, the second on the next one.
	for want := 0; want <= 1; want++ {
		next, _ := m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
		m = next.(model)
		if got := m.view.CurrentHunkIndex(); got != want {
			t.Fatalf("] moved to hunk %d, want %d", got, want)
		}
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m = next.(model)
	if got := m.view.CurrentHunkIndex(); got != 0 {
		t.Fatalf("[ moved to hunk %d, want 0", got)
	}
}

func TestEnterLoadsTheSelectedFile(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	// The sidebar owns enter, so it has focus for this one.
	m.showLeft = true
	m.setSize()
	m.focus = focusSidebar
	// Move the cursor without going through the list update path, exactly
	// like a mouse wheel or a programmatic selection would. Item 0 is the
	// section header, so index 3 is the third file.
	m.files.Select(3)

	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("enter should load the selected file")
	}
	if f, _ := m.files.SelectedFile(); f.Path != "c.go" {
		t.Fatalf("selected %q, want c.go", f.Path)
	}
	if m.focus != focusMain {
		t.Fatal("enter did not focus the main pane")
	}
}

func TestWelcomeReplacesStaleCommitDiff(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(1)
	m.setSize()
	files := []*diff.File{diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{})}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	m.files.SetCommits([]git.Commit{{Hash: "h", Short: "abc1234"}})

	// Back to files: there are no changed files, so the stale commit diff
	// must give way to the welcome screen.
	next, _ := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m = next.(model)
	if m.files.Tab() != 0 {
		t.Fatal("1 did not switch to the files tab")
	}
	if !m.showWelcome() {
		t.Fatal("welcome should hide the stale commit diff on a clean files tab")
	}
	if !m.view.Empty() {
		t.Fatal("stale commit diff was not cleared")
	}
}

func commitModel(smooth bool, width, height int) model {
	cfg := config.Default()
	cfg.SmoothScroll = smooth
	m := newModel(Options{Config: cfg, Source: git.Source{}}, "main", "twig")
	m.width, m.height = width, height
	m.loaded = true
	m.files.SetTab(1)
	m.setSize()

	var files []*diff.File
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		var oldText, newText strings.Builder
		for i := 0; i < 30; i++ {
			oldText.WriteString("line\n")
			if i == 10 {
				newText.WriteString("changed\n")
				continue
			}
			newText.WriteString("line\n")
		}
		files = append(files, diff.Generate(name, diff.Modified, oldText.String(), newText.String(),
			diff.GenerateOptions{Context: 3}))
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	m.files.SetCommits([]git.Commit{{Hash: "h", Short: "abc1234"}})
	return m
}

func TestCommitFileEnterJumpsToBand(t *testing.T) {
	m := commitModel(false, 120, 24)
	m.focus = focusCommitFiles
	m.files.SelectCommitFile(2)

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if m.focus != focusMain {
		t.Fatal("enter did not focus the main pane")
	}
	if got := m.view.YOffset(); got == 0 {
		t.Fatal("enter did not scroll to the selected file's band")
	}
}

func TestEnterOnCommitStartsAtTop(t *testing.T) {
	m := commitModel(false, 120, 24)
	m.focus = focusSidebar
	m.view.GotoBottom()
	if m.view.YOffset() == 0 {
		t.Fatal("test setup: commit diff should be scrollable")
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	nm := next.(model)
	if nm.focus != focusMain {
		t.Fatal("enter did not focus the main pane")
	}
	if nm.view.YOffset() != 0 {
		t.Fatalf("enter did not return to the commit start (offset %d)", nm.view.YOffset())
	}
}

func TestHeaderTabClickSwitchesTabs(t *testing.T) {
	m := testModel(120, 24)
	header := ansi.Strip(strings.Split(m.render(), "\n")[0])

	filesX, _, commitsX, _ := m.tabHitRanges()
	// The hit range includes the one-cell padding before the label.
	if got, want := strings.Index(header, "1 Files"), filesX+1; got != want {
		t.Fatalf("files tab drawn at %d, hit range starts at %d", got, filesX)
	}
	if got, want := strings.Index(header, "2 Commits"), commitsX+1; got != want {
		t.Fatalf("commits tab drawn at %d, hit range starts at %d", got, commitsX)
	}

	next, _ := m.Update(tea.MouseClickMsg{X: commitsX + 1, Y: 0})
	m = next.(model)
	if m.files.Tab() != filelist.TabCommits {
		t.Fatal("clicking the commits tab did not switch tabs")
	}
}

func TestHelpPillClickTogglesHelp(t *testing.T) {
	m := testModel(120, 24)
	next, _ := m.Update(tea.MouseClickMsg{X: m.width - 2, Y: m.height - 1})
	m = next.(model)
	if !m.help {
		t.Fatal("clicking the help pill did not open help")
	}

	next, _ = m.Update(tea.MouseClickMsg{X: m.width - 2, Y: m.height - 1})
	if next.(model).help {
		t.Fatal("clicking the help pill again did not close help")
	}
}

func TestMainPaneClickFocusesDiff(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusSidebar
	next, _ := m.Update(tea.MouseClickMsg{
		X: m.mainRect.Min.X + 2,
		Y: m.mainRect.Min.Y + 2,
	})
	if next.(model).focus != focusMain {
		t.Fatal("clicking the main pane did not focus it")
	}
}

func TestAllChangesBuildsAggregateAndJumps(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	m.focus = focusSidebar
	// Start on the last file so its band is somewhere below the first screen.
	m.files.SelectPath("c.go", false)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = next.(model)
	if !m.allChanges {
		t.Fatal("a did not enable the all-changes view")
	}
	if cmd == nil {
		t.Fatal("enabling all changes should load the aggregate")
	}

	// Feed the aggregate back, as the loader would. The files are tall
	// enough that the last band sits below the first screen.
	var oldText, newText strings.Builder
	for i := 0; i < 40; i++ {
		oldText.WriteString("line\n")
		if i == 20 {
			newText.WriteString("changed\n")
			continue
		}
		newText.WriteString("line\n")
	}
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3}),
		diff.Generate("c.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3}),
	}
	next, _ = m.Update(allLoadedMsg{file: diff.Aggregate(files)})
	m = next.(model)
	if m.view.File() == nil {
		t.Fatal("aggregate was not shown")
	}
	if got := m.view.YOffset(); got == 0 {
		t.Fatal("loading the aggregate should jump to the selected file's band")
	}

	// Enter on a file keeps the aggregate and jumps to its band instantly.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(model)
	if !m.allChanges {
		t.Fatal("enter left the all-changes view")
	}
	if got := m.view.YOffset(); got == 0 {
		t.Fatal("enter should jump to the selected file's band")
	}
}

func TestAllChangesStreamsChunks(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
		{Path: "c.go", Letter: "M"},
	})
	m.focus = focusSidebar
	// Select the last file: its band only exists once the final chunk lands.
	m.files.SelectPath("c.go", false)

	next, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = next.(model)
	if !m.allChanges || cmd == nil {
		t.Fatal("a did not start the all-changes load")
	}

	entries := m.files.OrderedEntries()
	tall := func(path string) *diff.File {
		var oldText strings.Builder
		for i := 0; i < 40; i++ {
			oldText.WriteString("line\n")
		}
		newText := strings.Replace(oldText.String(), "line\n", "changed\n", 1)
		return diff.Generate(path, diff.Modified, oldText.String(), newText, diff.GenerateOptions{Context: 3})
	}

	// First chunk: only a.go. The document shows immediately, the jump waits.
	next, cmd = m.Update(allChunkMsg{
		entries: entries,
		files:   []*diff.File{tall("a.go")},
		from:    0,
		gen:     m.allGen,
	})
	m = next.(model)
	if blocks, _ := m.view.DocBlocks(); blocks == 0 {
		t.Fatal("the first chunk was not shown")
	}
	if !m.allLoading || cmd == nil {
		t.Fatal("a partial load should schedule the next chunk")
	}
	if m.view.HasBand(2) {
		t.Fatal("a band was laid out for a file that has not loaded")
	}
	if !m.allPendingJump {
		t.Fatal("the jump to an unloaded selection should stay pending")
	}

	// Final chunk: the document grows, the pending jump lands on c.go.
	next, _ = m.Update(allChunkMsg{
		entries: entries,
		files:   []*diff.File{tall("b.go"), tall("c.go")},
		from:    1,
		gen:     m.allGen,
		done:    true,
	})
	m = next.(model)
	if m.allLoading {
		t.Fatal("the last chunk should finish the load")
	}
	if got := m.view.YOffset(); got == 0 {
		t.Fatal("the pending jump did not land on the selected file's band")
	}
	if m.allPendingJump {
		t.Fatal("the jump should no longer be pending")
	}

	// A chunk from a superseded load must not touch the document.
	before := m.view.YOffset()
	next, _ = m.Update(allChunkMsg{
		entries: entries,
		files:   []*diff.File{tall("a.go")},
		from:    0,
		gen:     m.allGen - 1,
	})
	m = next.(model)
	if got := m.view.YOffset(); got != before {
		t.Fatalf("stale chunk moved the view from %d to %d", before, got)
	}
}

// TestWelcomeTicksResumeAfterTabSwitch pins the splash animation coming back
// to life: leaving it for another tab ends the tick chain, and any later
// message that lands on the splash starts a fresh one.
func TestWelcomeTicksResumeAfterTabSwitch(t *testing.T) {
	m := testModel(100, 30)
	if !m.showWelcome() {
		t.Fatal("welcome should start visible")
	}

	next, cmd := m.Update(welcomeTickMsg{})
	if cmd == nil {
		t.Fatal("welcome tick should schedule the next frame")
	}
	m = next.(model)

	// Something to browse: the splash hides and the chain stops.
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	next, cmd = m.Update(welcomeTickMsg{})
	if cmd != nil {
		t.Fatal("welcome tick should stop while hidden")
	}
	m = next.(model)
	if m.welcomeTicking {
		t.Fatal("tick chain still marked in flight while hidden")
	}

	// Back to an empty list: the status load is what reveals the splash
	// again, and landing on it restarts the animation.
	next, cmd = m.Update(statusLoadedMsg{})
	nm := next.(model)
	if !nm.showWelcome() {
		t.Fatalf("empty status should show the splash (total %d)", nm.files.Total())
	}
	if cmd == nil {
		t.Fatal("landing on the splash should restart the animation")
	}
	if !nm.welcomeTicking {
		t.Fatal("restarted animation did not mark the tick chain in flight")
	}
	if nm.welcomeFrame != m.welcomeFrame {
		t.Fatal("restarting the animation should not skip frames")
	}
}

// TestHelpColumnsSeparateChrome checks the sidebars, help and quit keys get a
// column of their own on both tabs instead of trailing the navigation keys.
func TestHelpColumnsSeparateChrome(t *testing.T) {
	for _, tab := range []int{filelist.TabFiles, filelist.TabCommits} {
		m := testModel(120, 30)
		m.files.SetTab(tab)
		cols := m.helpColumns()

		last := cols[len(cols)-1]
		var got []string
		for _, e := range last {
			got = append(got, e.val)
		}
		want := []string{"sidebars", "hide help", "quit"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("tab %d chrome column = %v, want %v", tab, got, want)
		}

		for i, c := range cols[:len(cols)-1] {
			for _, e := range c {
				switch e.val {
				case "sidebars", "hide help", "quit":
					t.Fatalf("tab %d column %d still holds %q", tab, i, e.val)
				}
			}
		}
	}
}

// TestWelcomeStaysOnTheFilesTab pins the splash to the files tab: an empty
// commits list must not bring the animation back.
func TestWelcomeStaysOnTheFilesTab(t *testing.T) {
	m := testModel(100, 30)
	if !m.showWelcome() {
		t.Fatal("welcome should start visible on the files tab")
	}

	m.files.SetTab(filelist.TabCommits)
	if m.showWelcome() {
		t.Fatal("welcome should not show on the commits tab")
	}
	next, cmd := m.Update(welcomeTickMsg{})
	if cmd != nil {
		t.Fatal("welcome tick should stop once the commits tab is active")
	}
	if next.(model).welcomeFrame != m.welcomeFrame {
		t.Fatal("welcome frame advanced while the commits tab was active")
	}

	m.files.SetTab(filelist.TabFiles)
	if !m.showWelcome() {
		t.Fatal("welcome should come back with the files tab")
	}
}

// TestCommitNotesCarryTheBody checks the selected commit's body shows in the
// diff pane under the subject.
func TestCommitNotesCarryTheBody(t *testing.T) {
	m := testModel(100, 30)
	const full = "0c1510f8d448000000000000000000000000000000"
	notes := strings.Join(commitNotes(m.styles, git.Commit{
		Hash:        full,
		Short:       "0c1510f",
		Author:      "sam",
		When:        "Oct 29, 2026",
		Subject:     "subject here",
		Description: "the body line\nand a second one",
	}, nil), "\n")

	// The metadata line carries the whole hash, and the body keeps every line.
	for _, want := range []string{full, "sam", "subject here", "the body line", "and a second one"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("commit notes missing %q:\n%s", want, notes)
		}
	}
	if strings.HasPrefix(ansi.Strip(notes), " ") {
		t.Fatalf("commit notes start with a margin:\n%s", notes)
	}

	// A bodyless commit leaves no empty note behind.
	bare := strings.Join(commitNotes(m.styles, git.Commit{Hash: full, Subject: "no body"}, nil), "\n")
	if strings.Count(bare, "\n") != 2 {
		t.Fatalf("bodyless notes have stray lines:\n%s", bare)
	}
}

// TestLogoPillSpansTwoColors checks the logo gradient starts on Hazy and ends
// on Salmon, with the two tones as its only stops.
func TestLogoPillSpansTwoColors(t *testing.T) {
	if len(logoStops) != 2 {
		t.Fatalf("logo uses %d colors, want 2", len(logoStops))
	}
	cols := blendStops(8, logoStops)
	if len(cols) != 8 {
		t.Fatalf("blend produced %d colors", len(cols))
	}
	for _, tc := range []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{"first", cols[0], tone.Hazy},
		{"last", cols[len(cols)-1], tone.Salmon},
	} {
		gr, gg, gb, _ := tc.got.RGBA()
		wr, wg, wb, _ := tc.want.RGBA()
		if gr != wr || gg != wg || gb != wb {
			t.Fatalf("%s stop = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestSmoothScrollTogglesAndPersists checks the global smooth-scroll key both
// flips the animation and writes the choice to the config file.
func TestSmoothScrollTogglesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twig.json")
	seeded := config.Default()
	seeded.SmoothScroll = false
	if err := config.Save(path, seeded); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	m := testModel(100, 30)
	m.cfg.SmoothScroll = false
	m.view.SetSmooth(false)
	m.cfgPath = path
	m.cfgStamp = configStamp(path)
	m.focus = focusMain

	next, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	nm := next.(model)
	if !nm.cfg.SmoothScroll {
		t.Fatal("m did not enable smooth scrolling")
	}
	if !nm.view.Smooth() {
		t.Fatal("the diff pane did not get the new setting")
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.SmoothScroll {
		t.Fatal("the toggle was not written to the config file")
	}
}

// TestConfigReloadsOnExternalEdit checks an edit made outside twig lands in the
// running app on the next config tick.
func TestConfigReloadsOnExternalEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twig.json")
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	m := testModel(100, 30)
	m.cfgPath = path
	m.cfgStamp = configStamp(path)

	edited := config.Default()
	edited.Wrap = true
	edited.Context = 8
	if err := config.Save(path, edited); err != nil {
		t.Fatalf("edit config: %v", err)
	}
	// A same-size rewrite can share the mtime tick, so age the stamp.
	m.cfgStamp = "stale"

	next, cmd := m.Update(configTickMsg{})
	nm := next.(model)
	if !nm.cfg.Wrap || nm.cfg.Context != 8 {
		t.Fatalf("config not reloaded: wrap=%v context=%d", nm.cfg.Wrap, nm.cfg.Context)
	}
	if cmd == nil {
		t.Fatal("the watcher should keep ticking")
	}
	if nm.message != "config reloaded" {
		t.Fatalf("message = %q", nm.message)
	}
}

// TestConfigWatchStopsWithoutAFile checks an empty config path does not keep a
// tick chain alive.
func TestConfigWatchStopsWithoutAFile(t *testing.T) {
	m := testModel(100, 30)
	next, cmd := m.Update(configTickMsg{})
	if cmd != nil {
		t.Fatal("no config path should not keep the watcher running")
	}
	if next.(model).configTicking {
		t.Fatal("watcher marked in flight without a config path")
	}
}

// TestHeaderCountsFlankTheBranchSegment pins the bar layout: the branch and
// view label is its own segment, additions sit on its left, deletions on its
// right, and the tabs close the line.
func TestHeaderCountsFlankTheBranchSegment(t *testing.T) {
	m := testModel(100, 24)
	m.d = &diff.Diff{Additions: 12, Deletions: 3}

	line := ansi.Strip(m.headerView())
	adds := strings.Index(line, "+12")
	branch := strings.Index(line, "main · worktree")
	dels := strings.Index(line, "-3")
	tabs := strings.Index(line, "2 Commits")
	for name, idx := range map[string]int{"+12": adds, "branch": branch, "-3": dels, "tabs": tabs} {
		if idx < 0 {
			t.Fatalf("header missing %s:\n%q", name, line)
		}
	}
	if adds >= branch || branch >= dels || dels >= tabs {
		t.Fatalf("header order is wrong: +12=%d branch=%d -3=%d tabs=%d\n%q",
			adds, branch, dels, tabs, line)
	}
	if trailing := len(line) - len(strings.TrimRight(line, " ")); trailing > 1 {
		t.Fatalf("header has %d trailing spaces, want only the pill padding:\n%q", trailing, line)
	}
}

// TestTopBarSegmentsUseSalt checks every bar segment writes its text in Salt,
// whatever background it sits on.
func TestTopBarSegmentsUseSalt(t *testing.T) {
	st := styles.New()
	for name, style := range map[string]lipgloss.Style{
		"tab":         st.HeaderTab(),
		"active tab":  st.HeaderTabActive(),
		"adds":        st.HeaderAdds(),
		"dels":        st.HeaderDels(),
		"files key":   st.FooterKeyFiles(),
		"diff key":    st.FooterKeyDiff(),
		"commits key": st.FooterKeyCommits(),
		"commit key":  st.FooterKeyCommit(),
		"value":       st.FooterValue(),
		"info":        st.FooterInfo(),
		"help":        st.FooterHelp(),
		"message":     st.FooterMessage(),
	} {
		got := style.GetForeground()
		gr, gg, gb, _ := got.RGBA()
		wr, wg, wb, _ := tone.Salt.RGBA()
		if gr != wr || gg != wg || gb != wb {
			t.Fatalf("%s foreground = %v, want Salt", name, got)
		}
	}
}

// TestRightSidebarFilterDoesNotTouchTheLeft pins the sidebar separation: with
// the commits list filtered, typing in the per-commit sidebar filters that
// sidebar, not the commits list.
func TestRightSidebarFilterDoesNotTouchTheLeft(t *testing.T) {
	m := testModel(120, 24)
	m.showLeft = true
	m.showRight = true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()
	m.files.SetCommits([]git.Commit{
		{Hash: "aaa", Short: "aaa1111", Subject: "first", Author: "sam", When: "Oct 01, 26"},
		{Hash: "bbb", Short: "bbb2222", Subject: "second", Author: "sam", When: "Oct 02, 26"},
	})

	files := []*diff.File{
		diff.Generate("alpha.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("beta.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	m.focus = focusCommitFiles

	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(model)
	if !m.files.CommitFilesFiltering() {
		t.Fatal("/ did not start the commit-file filter")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = next.(model)

	if !m.files.CommitFilesFiltering() {
		t.Fatal("commit files list is not filtering")
	}
	if got := m.files.FilterStateDebug(); got != 0 { // list.Unfiltered
		t.Fatalf("commits list filter state = %v, want unfiltered", got)
	}
}

// TestTypingAfterClickingTheRightSidebarStaysThere reproduces the reported
// flow: the commits list is mid-filter, the user clicks a commit file, types,
// and the keystrokes must land in the per-commit sidebar.
func TestTypingAfterClickingTheRightSidebarStaysThere(t *testing.T) {
	m := testModel(120, 24)
	m.showLeft = true
	m.showRight = true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()
	m.files.SetCommits([]git.Commit{
		{Hash: "aaa", Short: "aaa1111", Subject: "first commit", Author: "sam", When: "Oct 01, 26"},
		{Hash: "bbb", Short: "bbb2222", Subject: "second commit", Author: "sam", When: "Oct 02, 26"},
	})
	files := []*diff.File{
		diff.Generate("alpha.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("beta.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	m.setSize()

	// Left sidebar: open its filter and type something, without applying it.
	m.focus = focusSidebar
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(model)
	next, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = next.(model)
	if !m.files.FilterActive() {
		t.Fatal("commits filter did not open")
	}

	// Click a commit file in the right sidebar.
	next, _ = m.Update(tea.MouseClickMsg{
		X: m.commitFilesRct.Min.X + 2,
		Y: m.commitFilesRct.Min.Y + 2,
	})
	m = next.(model)
	if m.focus != focusCommitFiles {
		t.Fatalf("click did not focus the commit sidebar (focus %v)", m.focus)
	}

	// Type: it must reach the per-commit sidebar, and only it.
	for _, r := range "/b" {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(model)
	}
	if got := m.files.CommitFilesFilterText(); got != "b" {
		t.Fatalf("commit files filter = %q, want the typed %q", got, "b")
	}
	if !m.files.CommitFilesFiltering() {
		t.Fatal("commit files list is not filtering")
	}
	if got := m.files.FilterStateDebug(); got != 1 { // still Filtering, not reset
		t.Fatalf("commits list filter state = %v, want it left alone", got)
	}
}

// TestDeleteKeyStaysInTheSearchInput pins the delete key: it edits the query
// instead of acting as "back" and closing the search.
func TestDeleteKeyStaysInTheSearchInput(t *testing.T) {
	m := testModel(100, 30)
	m.focus = focusMain
	next, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = next.(model)
	if !m.search.Active() {
		t.Fatal("search did not open")
	}
	for _, r := range "abc" {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(model)
	}
	// Backspace once, then delete forward.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = next.(model)
	if !m.search.Active() || m.search.Value() != "ab" {
		t.Fatalf("backspace left search active=%t value=%q", m.search.Active(), m.search.Value())
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	m = next.(model)
	if !m.search.Active() {
		t.Fatal("delete closed the search")
	}
}

// TestCommitNotesListTheChangedFiles checks the commit pane names the files it
// touched, with their line counts, above the diff.
func TestCommitNotesListTheChangedFiles(t *testing.T) {
	m := testModel(120, 30)
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{}),
		diff.Generate("dir/b.go", diff.Added, "", "x\ny\n", diff.GenerateOptions{}),
	}
	notes := ansi.Strip(strings.Join(commitNotes(m.styles, git.Commit{
		Hash: "abc", Subject: "change things",
	}, files), "\n"))

	for _, want := range []string{"2 files changed", "a.go", "+1", "-1", "dir/b.go", "+2", "-0"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("commit notes missing %q:\n%s", want, notes)
		}
	}

	// Unknown counts (a lazily loaded commit) leave the line bare.
	skeleta := []*diff.File{{NewPath: "late.go", Additions: -1, Deletions: -1}}
	notes = ansi.Strip(strings.Join(commitNotes(m.styles, git.Commit{Subject: "lazy"}, skeleta), "\n"))
	if !strings.Contains(notes, "late.go") || strings.Contains(notes, "+-1") {
		t.Fatalf("lazy file line = %q", notes)
	}
}

// TestWrapAndSplitTurnEachOtherOff pins the layout invariant: wrap needs the
// full width, so enabling either one disables the other instead of leaving a
// split view that quietly refuses to wrap.
func TestWrapAndSplitTurnEachOtherOff(t *testing.T) {
	m := testModel(120, 30)
	m.focus = focusMain

	split := tea.KeyPressMsg{Code: 'v', Text: "v"}
	wrap := tea.KeyPressMsg{Code: 'w', Text: "w"}

	next, _ := m.Update(split)
	m = next.(model)
	if !m.view.Split() {
		t.Fatal("v did not enable the split view")
	}
	next, _ = m.Update(wrap)
	m = next.(model)
	if m.view.Split() {
		t.Fatal("wrap left the split view on")
	}
	if m.cfg.SplitView {
		t.Fatal("config still claims split view")
	}
	if !m.cfg.Wrap {
		t.Fatal("wrap is not on")
	}

	next, _ = m.Update(split)
	m = next.(model)
	if !m.view.Split() {
		t.Fatal("v did not re-enable the split view")
	}
	if m.cfg.Wrap {
		t.Fatal("split view left wrap on")
	}
}

// TestSidebarConfigIsHonouredAndSaved covers the two visibility settings: they
// decide what the window opens with, the keys flip them from there, and a flip
// is written back so the choice survives a restart.
func TestSidebarConfigIsHonouredAndSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twig.json")

	cfg := config.Default()
	cfg.ShowLeftSidebar = true
	cfg.ShowRightSidebar = true

	m := newModel(Options{Config: cfg, ConfigPath: path}, "main", "twig")
	m.width, m.height = 120, 24
	m.loaded = true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()

	if !m.showLeft || m.sideRect.Dx() == 0 {
		t.Fatal("the left sidebar was configured visible but is not")
	}
	if !m.showRight || m.commitFilesRct.Dx() == 0 {
		t.Fatal("the right sidebar was configured visible but is not")
	}
	assertGeometry(t, m, "both sidebars from config")

	next, _ := m.Update(tea.KeyPressMsg{Code: 'H', Text: "H"})
	m = next.(model)
	if m.showLeft {
		t.Fatal("H did not hide the left sidebar")
	}
	saved, err := config.Load(path)
	if err != nil {
		t.Fatalf("the toggle did not write a config: %v", err)
	}
	if saved.ShowLeftSidebar {
		t.Fatal("the hidden left sidebar was not saved")
	}
	if !saved.ShowRightSidebar {
		t.Fatal("hiding one sidebar disturbed the other")
	}

	// A reload from disk follows the file, including the side that was just
	// hidden by hand.
	next, _ = m.Update(statusLoadedMsg{fingerprint: "x"})
	m = next.(model)
	if m.showLeft {
		t.Fatal("a reload brought back the hidden sidebar")
	}
}

// TestSidebarReloadFollowsTheFile covers the other direction: editing the
// config on disk has to move the panes without a restart.
func TestSidebarReloadFollowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twig.json")
	cfg := config.Default()
	m := newModel(Options{Config: cfg, ConfigPath: path}, "main", "twig")
	m.width, m.height = 120, 24
	m.loaded = true
	m.setSize()

	if m.showLeft {
		t.Fatal("the left sidebar starts hidden by default")
	}
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatal(err)
	}
	cfg.ShowLeftSidebar = true
	if cmd := m.applyConfig(cfg); cmd != nil {
		t.Fatal("showing a sidebar should not need a reload of the content")
	}
	if !m.showLeft || m.sideRect.Dx() == 0 {
		t.Fatal("a config reload did not show the left sidebar")
	}
}
