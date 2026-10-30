package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// runCmd executes a command and its batches without waiting on timers: the
// filter commands answer immediately, ticks are simply dropped.
func runCmd(m model, cmd tea.Cmd, depth int) model {
	if cmd == nil || depth > 5 {
		return m
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				m = runCmd(m, c, depth+1)
			}
			return m
		case nil:
			return m
		default:
			next, c := m.Update(v)
			return runCmd(next.(model), c, depth+1)
		}
	case <-time.After(20 * time.Millisecond):
		return m
	}
}

// TestCommitFileFilterNarrowsTheRightSidebar pins the routing bug: match
// results for the per-commit sidebar used to be delivered to the commits
// listing, so the right pane never filtered and the left list changed instead.
func TestCommitFileFilterNarrowsTheRightSidebar(t *testing.T) {
	m := testModel(120, 24)
	m.showLeft = true
	m.showRight = true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()
	m.files.SetCommits([]git.Commit{
		{Hash: "a", Short: "aaa1111", Subject: "first commit", Author: "sam", When: "Oct 01, 26"},
		{Hash: "b", Short: "bbb2222", Subject: "second commit", Author: "sam", When: "Oct 02, 26"},
	})
	files := []*diff.File{
		diff.Generate("alpha.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{}),
		diff.Generate("beta.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{}),
	}
	m.applyCommit(git.Commit{Hash: "h", Short: "abc1234"}, diff.Aggregate(files), files)
	m.setSize()

	send := func(msg tea.Msg) {
		next, cmd := m.Update(msg)
		m = runCmd(next.(model), cmd, 0)
	}

	// Open the commits filter and type into it, leaving it unapplied.
	m.focus = focusSidebar
	send(tea.KeyPressMsg{Code: '/', Text: "/"})
	send(tea.KeyPressMsg{Code: 'c', Text: "c"})
	leftBefore := m.files.VisibleTotal()

	// Click into the per-commit sidebar and filter it.
	send(tea.MouseClickMsg{X: m.commitFilesRct.Min.X + 2, Y: m.commitFilesRct.Min.Y + 2})
	if m.focus != focusCommitFiles {
		t.Fatalf("click did not focus the commit sidebar (focus %v)", m.focus)
	}
	for _, r := range "/be" {
		send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	view := ansi.Strip(m.files.CommitFilesView())
	if !strings.Contains(view, "beta.go") {
		t.Fatalf("beta.go missing from the filtered commit sidebar:\n%s", view)
	}
	if strings.Contains(view, "alpha.go") {
		t.Fatalf("alpha.go survived a filter for be:\n%s", view)
	}
	// The commits listing is untouched by the right sidebar's filter.
	if got := m.files.VisibleTotal(); got != leftBefore {
		t.Fatalf("commits listing changed: %d rows, was %d", got, leftBefore)
	}
}
