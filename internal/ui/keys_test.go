package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/filelist"
	"samouly.fun/twig/internal/ui/components/statusbar"
	"samouly.fun/twig/internal/ui/styles"
)

// TestViewKeysWorkFromEitherPane: keys about the view rather than the cursor
// belong to the window, so they must work with the sidebar focused instead of
// doing nothing because the reader happened to be in the other pane.
func TestViewKeysWorkFromEitherPane(t *testing.T) {
	for _, focus := range []focus{focusSidebar, focusMain} {
		m := testModel(120, 24)
		m.focus = focus
		m.view.SetFile(diff.Generate("a.go", diff.Modified,
			"one\n", "one\ntwo\n", diff.GenerateOptions{Context: 3}))

		next, cmd := m.Update(keyPress("y"))
		got := next.(model)
		if !strings.Contains(got.message, "copied") {
			t.Fatalf("focus %v: yank said %q", focus, got.message)
		}
		if cmd == nil {
			t.Fatalf("focus %v: yank scheduled nothing", focus)
		}

		// Context size and whitespace are view settings: they must be
		// reachable from the same places.
		before := got.cfg.Context
		next, _ = got.Update(keyPress("c"))
		if after := next.(model).cfg.Context; after == before {
			t.Fatalf("focus %v: c did not cycle the context", focus)
		}
		ws := next.(model).cfg.IgnoreWhitespace
		next, _ = next.(model).Update(keyPress("Z"))
		if next.(model).cfg.IgnoreWhitespace == ws {
			t.Fatalf("focus %v: Z did not toggle whitespace", focus)
		}
	}
}

func TestFocusCyclesAndMutesTheDiff(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Borderless = true
	m.view.SetBorderless(true)
	m.showLeft = true
	m.showRight = true
	m.files.SetTab(filelist.TabCommits)
	m.setSize()
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
	}
	m.applyCommit(git.Commit{Hash: "1", Short: "abc1234", Subject: "one", Author: "sam", When: "now"},
		diff.Aggregate(files), files)
	m.files.SetCommits([]git.Commit{{Hash: "1", Short: "abc1234", Subject: "one"}})
	if len(m.commitFileList) == 0 {
		t.Fatal("test setup: commit has no file list")
	}

	m.focus = focusMain
	m.syncStaging()
	full := m.mainPane()
	activeAdd := m.styles.DiffAdd().Render("+b")
	if !strings.Contains(full, activeAdd) {
		t.Fatalf("focused diff does not show active colors:\n%s", full)
	}

	for _, want := range []focus{focusCommitFiles, focusSidebar, focusMain} {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(model)
		if m.focus != want {
			t.Fatalf("focus = %v, want %v", m.focus, want)
		}
		if focused := want == focusMain; m.view.CursorOn() != focused {
			t.Fatalf("focus = %v: diff cursor focus = %v", m.focus, m.view.CursorOn())
		}
		got := m.mainPane()
		if focused := want == focusMain; focused != strings.Contains(got, activeAdd) {
			t.Fatalf("focus = %v: diff colors did not follow focus", m.focus)
		}
		if focused := want == focusMain; focused != (got == full) {
			t.Fatalf("focus = %v: main pane did not return to its focused rendering", m.focus)
		}
	}
}

func TestFoldKeyTogglesCommitMetadata(t *testing.T) {
	m := testModel(80, 20)
	m.view.SetNotes([]string{"hash sam", "subject", "body"})
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}))
	before := m.view.Rows()

	next, _ := m.Update(keyPress("o"))
	m = next.(model)
	if got := m.view.Rows(); got != before-2 {
		t.Fatalf("fold key rows = %d, want %d", got, before-2)
	}
	if strings.Contains(m.view.View(), "body") {
		t.Fatal("fold key left metadata visible")
	}

	next, _ = m.Update(keyPress("o"))
	m = next.(model)
	if got := m.view.Rows(); got != before {
		t.Fatalf("unfold key rows = %d, want %d", got, before)
	}
}

// TestHelpListsEveryKeyThatWorks keeps the reference honest: a key the model
// handles but the help omits is a key the reader has to remember.
func TestVRangeCopiesSelectedLines(t *testing.T) {
	m := testModel(80, 20)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "one\ntwo\nthree\n", "one\nTWO\nthree\n", diff.GenerateOptions{Context: 3}))
	m.focus = focusMain
	m.syncStaging()

	next, _ := m.Update(keyPress("V"))
	m = next.(model)
	next, _ = m.Update(keyPress("j"))
	m = next.(model)
	next, cmd := m.Update(keyPress("y"))
	m = next.(model)
	if cmd == nil || m.message != "copied selection" {
		t.Fatalf("V+y did not copy selected lines: message %q, cmd %v", m.message, cmd != nil)
	}
}

// TestYankHunkSaysWhatItCopied: every yank reports through the footer, so the
// hunk key must not be the one silent case.
func TestYankHunkSaysWhatItCopied(t *testing.T) {
	m := testModel(120, 24)
	m.lastLoaded.path = "a.go"
	m.focus = focusMain
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"one\n", "one\ntwo\n", diff.GenerateOptions{Context: 3}))
	m.syncStaging()

	next, cmd := m.Update(keyPress("Y"))
	got := next.(model)
	if got.message != "copied hunk" {
		t.Fatalf("hunk yank said %q, want a notification", got.message)
	}
	if cmd == nil {
		t.Fatal("hunk yank scheduled nothing")
	}
}

// TestNoticesPickTheirTone: the footer notice draws its colour from the kind of
// outcome the message names.
func TestNoticesPickTheirTone(t *testing.T) {
	cases := map[string]styles.NoticeKind{
		"copied patch":             styles.NoticeOkay,
		"copied hunk":              styles.NoticeOkay,
		"committed abc1234":        styles.NoticeOkay,
		"pushed!":                  styles.NoticeOkay,
		"undid stage":              styles.NoticeOkay,
		"nothing to copy":          styles.NoticeWarn,
		"no matches":               styles.NoticeWarn,
		"live refresh unavailable": styles.NoticeWarn,
		"error: fatal: denied":     styles.NoticeFail,
		"context lines: 5":         styles.NoticeInfo,
	}
	for text, want := range cases {
		if got := noticeKindOf(text); got != want {
			t.Fatalf("noticeKindOf(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestNoticeScrambleTicksUntilItSettles: a fresh message starts a short
// animation and stops ticking once the text has settled.
func TestNoticeScrambleTicksUntilItSettles(t *testing.T) {
	m := testModel(100, 24)
	m.lastLoaded.path = "a.go"
	m.focus = focusMain
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "one\n", "one\ntwo\n", diff.GenerateOptions{Context: 3}))
	m.syncStaging()

	next, cmd := m.Update(keyPress("y"))
	m = next.(model)
	if m.message == "" || !m.noticeTicking {
		t.Fatalf("yank did not start a notice: message %q ticking %v", m.message, m.noticeTicking)
	}
	if cmd == nil {
		t.Fatal("notice scramble scheduled nothing")
	}

	// Once the window has passed, the next frame ends the animation.
	m.noticeSince = time.Now().Add(-statusbar.NoticeDuration - time.Millisecond)
	next, cmd = m.Update(noticeTickMsg{})
	m = next.(model)
	if m.noticeTicking || cmd != nil {
		t.Fatalf("notice kept ticking after it settled: ticking %v cmd %v", m.noticeTicking, cmd != nil)
	}
}

// TestNoticeLivesExactlyThreeSeconds: the exit scramble is part of the notice's
// budget, not extra time on top.
func TestNoticeLivesExactlyThreeSeconds(t *testing.T) {
	if got := messageHold + statusbar.NoticeDuration; got != statusMessageTimeout {
		t.Fatalf("notice lives %v, want %v", got, statusMessageTimeout)
	}
}

// TestNoticeScramblesOutBeforeClearing: the clear timeout starts the way-out
// animation and only empties the message once it has played.
func TestNoticeScramblesOutBeforeClearing(t *testing.T) {
	m := testModel(100, 24)
	m.lastLoaded.path = "a.go"
	m.focus = focusMain
	m.view.SetFile(diff.Generate("a.go", diff.Modified, "one\n", "one\ntwo\n", diff.GenerateOptions{Context: 3}))
	m.syncStaging()
	next, _ := m.Update(keyPress("y"))
	m = next.(model)

	next, _ = m.Update(clearMsgMsg{})
	m = next.(model)
	if m.message == "" {
		t.Fatal("clear wiped the notice instead of scrambling it out")
	}
	if m.noticeLeft.IsZero() || !m.noticeTicking {
		t.Fatalf("clear did not start the exit scramble: left %v ticking %v",
			m.noticeLeft, m.noticeTicking)
	}

	// Let the exit animation run out: the next frame past its duration clears.
	m.noticeLeft = time.Now().Add(-statusbar.NoticeDuration - time.Millisecond)
	next, cmd := m.Update(noticeTickMsg{})
	m = next.(model)
	if m.message != "" || m.noticeTicking || cmd != nil {
		t.Fatalf("notice outlived its exit: message %q ticking %v cmd %v",
			m.message, m.noticeTicking, cmd != nil)
	}
}

// TestCommitsArrowsScrollTheView: the commits tab is a normal reader, so the
// arrows scroll it, and the rail follows the section they scroll to.
func TestCommitsArrowsScrollTheView(t *testing.T) {
	m := testModel(120, 20)
	m.files.SetTab(filelist.TabCommits)
	m.setSize()
	file := diff.Generate("a.go", diff.Modified, twoHunkSource(false), twoHunkSource(true), diff.GenerateOptions{Context: 3})
	m.applyCommit(git.Commit{Hash: "1", Short: "abc1234", Subject: "one", Author: "sam", When: "now"},
		diff.Aggregate([]*diff.File{file}), []*diff.File{file})
	m.view.SetNotes([]string{"commit metadata", "subject"})
	m.focus = focusMain
	m.syncStaging()

	if got := m.view.ReadRow(); got != 0 {
		t.Fatalf("rail starts on %d, want the commit metadata 0", got)
	}
	before := m.view.YOffset()
	next, _ := m.Update(keyPress("j"))
	m = next.(model)
	if got := m.view.YOffset(); got == before {
		t.Fatal("j did not scroll the commits pane")
	}
}

// twoHunkSource is a file with two changes far enough apart to make two hunks.
func twoHunkSource(changed bool) string {
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		switch {
		case changed && i == 3:
			b.WriteString("after\n")
		case changed && i == 30:
			b.WriteString("later\n")
		case !changed && i == 3:
			b.WriteString("before\n")
		case !changed && i == 30:
			b.WriteString("earlier\n")
		default:
			b.WriteString("line\n")
		}
	}
	return b.String()
}

func TestHelpListsEveryKeyThatWorks(t *testing.T) {
	filesTab := testModel(120, 30)
	commitsTab := testModel(120, 30)
	commitsTab.files.SetTab(1)

	files := helpValues(filesTab.helpColumns())
	for _, want := range []string{
		"down", "up", "top", "bottom", "prev hunk", "next hunk",
		"next file", "prev file", "tabs", "open", "next pane", "prev pane",
		"fold", "back", "find", "next match", "prev match", "yank patch", "yank hunk",
		"stage here", "undo", "discard", "stage file",
		"unstage", "commit", "split view", "wrap", "ignore ws", "context",
		"smooth", "tree view", "all changes", "push", "refresh",
		"sidebars", "hide help", "quit",
	} {
		if !files[want] {
			t.Errorf("files help omits %q", want)
		}
	}
	// The apply key only exists in select mode.
	if files["apply marks"] {
		t.Error("files help lists apply marks in instant mode")
	}

	commits := helpValues(commitsTab.helpColumns())
	for _, want := range []string{
		"down", "up", "top", "bottom", "prev hunk", "next hunk", "next file",
		"prev file", "open commit", "next pane", "prev pane", "fold", "back", "tabs",
		"find", "next match", "prev match", "yank patch", "yank hunk",
		"split view", "wrap", "ignore ws", "context", "smooth", "refresh",
		"commit", "push", "sidebars", "hide help", "quit",
	} {
		if !commits[want] {
			t.Errorf("commits help omits %q", want)
		}
	}
	if commits["stage here"] {
		t.Error("commits help lists staging, which is not offered there")
	}
}

// TestHelpListsApplyMarksInSelectMode covers the one entry that depends on the
// configured apply model.
func TestHelpListsApplyMarksInSelectMode(t *testing.T) {
	m := testModel(120, 30)
	m.cfg.StageApply = "select"
	if !helpValues(m.helpColumns())["apply marks"] {
		t.Fatal("select mode does not list the apply key")
	}
}

func helpValues(cols []helpColumn) map[string]bool {
	out := make(map[string]bool)
	for _, c := range cols {
		for _, e := range c {
			out[e.val] = true
		}
	}
	return out
}
