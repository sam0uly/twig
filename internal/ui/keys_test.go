package ui

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
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

// TestHelpListsEveryKeyThatWorks keeps the reference honest: a key the model
// handles but the help omits is a key the reader has to remember.
func TestHelpListsEveryKeyThatWorks(t *testing.T) {
	filesTab := testModel(120, 30)
	commitsTab := testModel(120, 30)
	commitsTab.files.SetTab(1)

	files := helpValues(filesTab.helpColumns())
	for _, want := range []string{
		"down", "up", "top", "bottom", "prev hunk", "next hunk",
		"next file", "prev file", "tabs", "open", "next pane", "prev pane",
		"back", "find", "next match", "prev match", "yank patch", "yank hunk",
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
		"prev file", "open commit", "next pane", "prev pane", "back", "tabs",
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
