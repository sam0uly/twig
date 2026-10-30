package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/git"
)

// TestPushShowsASpinnerUntilItReturns covers a push, which talks to the
// network: the footer has to say it is working, and stop saying it when the
// push returns.
func TestPushShowsASpinnerUntilItReturns(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})

	next, cmd := m.startPush()
	pushing := next.(model)
	if !pushing.pushing {
		t.Fatal("the push was not marked in flight")
	}
	if cmd == nil {
		t.Fatal("the push command was not scheduled")
	}
	if !strings.Contains(ansi.Strip(pushing.render()), "pushing") {
		t.Fatalf("the footer does not show the push:\n%s", ansi.Strip(pushing.render()))
	}

	// a second push while one is in flight would race the first
	if _, cmd := pushing.startPush(); cmd != nil {
		t.Fatal("a second push was scheduled while one was in flight")
	}

	next, _ = pushing.Update(pushedMsg{})
	done := next.(model)
	if done.pushing {
		t.Fatal("the push stayed marked in flight after returning")
	}
	if done.message != "pushed!" {
		t.Fatalf("message = %q, want the confirmation", done.message)
	}
	if strings.Contains(ansi.Strip(done.render()), "pushing") {
		t.Fatalf("the footer still shows the push:\n%s", ansi.Strip(done.render()))
	}
}

// TestPushNoticeHasNoStylingOfItsOwn covers the footer highlight: the message
// segment is painted with one background, so a colour reset inside the text
// would end that background at the spinner and leave the rest of the line bare.
func TestPushNoticeHasNoStylingOfItsOwn(t *testing.T) {
	m := testModel(120, 24)
	notice := m.pushNotice()

	if strings.Contains(notice, "\x1b") {
		t.Fatalf("the notice carries escape sequences: %q", notice)
	}
	if !strings.Contains(notice, "pushing") {
		t.Fatalf("notice = %q, want the label", notice)
	}
	if ansi.StringWidth(notice) <= ansi.StringWidth(" pushing…") {
		t.Fatalf("notice = %q, want the spinner glyph in front", ansi.Strip(notice))
	}
}
