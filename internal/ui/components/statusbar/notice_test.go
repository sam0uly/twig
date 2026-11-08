package statusbar

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/ui/styles"
)

func testFooter(notice Notice) string {
	m := New(styles.New())
	m.SetWidth(80)
	return m.FooterView(Info{Notice: &notice})
}

// TestNoticeFillsTheBar: whatever the frame, the notice occupies exactly the
// cells the message segment owns, so the footer never shifts.
func TestNoticeFillsTheBar(t *testing.T) {
	base := time.Unix(1700000000, 0)
	for _, elapsed := range []time.Duration{0, 60 * time.Millisecond, 125 * time.Millisecond, NoticeDuration, time.Second} {
		bar := testFooter(Notice{
			Kind:  styles.NoticeOkay,
			Body:  "copied hunk",
			Since: base,
			Now:   base.Add(elapsed),
		})
		for i, line := range strings.Split(bar, "\n") {
			if w := ansi.StringWidth(line); w != 80 {
				t.Fatalf("elapsed %v: line %d is %d cells, want 80", elapsed, i, w)
			}
		}
	}
}

// TestNoticeScramblesThenSettles: the body starts as noise, resolves left to
// right, and lands on the real text once the scramble is over.
func TestNoticeScramblesThenSettles(t *testing.T) {
	m := New(styles.New())
	m.SetWidth(80)
	base := time.Unix(1700000000, 0)
	body := "copiedhunk"

	frame := func(elapsed time.Duration) string {
		bar := m.FooterView(Info{Notice: &Notice{
			Kind:  styles.NoticeOkay,
			Body:  body,
			Since: base,
			Now:   base.Add(elapsed),
		}})
		plain := ansi.Strip(bar)
		plain = plain[strings.Index(plain, "OKAY!")+len("OKAY!"):]
		return strings.TrimSpace(plain)
	}

	if got := frame(0); strings.Contains(got, "copied") {
		t.Fatalf("body settled before it scrambled: %q", got)
	}
	if got := frame(NoticeDuration); !strings.Contains(got, body) {
		t.Fatalf("body did not settle after %v: %q", NoticeDuration, got)
	}
	// Halfway, some positions have settled and some are still noise, and they
	// are scattered rather than sweeping from the left.
	mid := []rune(frame(NoticeDuration / 2))
	want := []rune(body)
	settled := 0
	for i := 0; i < len(want) && i < len(mid); i++ {
		if mid[i] == want[i] {
			settled++
		}
	}
	if settled == 0 {
		t.Fatalf("halfway body settled nothing: %q", string(mid))
	}
	if settled >= len(want) {
		t.Fatalf("halfway body is already whole: %q", string(mid))
	}
	// A body that resolved from the left would show the same opening letters
	// at every frame; a scattered one need not.
	startsLeft := func(text string) bool {
		r := []rune(text)
		return len(r) >= 3 && string(r[:3]) == string(want[:3])
	}
	if startsLeft(frame(NoticeDuration/4)) && startsLeft(string(mid)) {
		t.Fatalf("body resolves from the left instead of scattered: %q", string(mid))
	}
}

// TestNoticeScramblesOut: a leaving notice dissolves into noise instead of
// blinking away, and is all noise once its time is up.
func TestNoticeScramblesOut(t *testing.T) {
	m := New(styles.New())
	m.SetWidth(80)
	base := time.Unix(1700000000, 0)
	body := "copiedhunk"
	notice := func(left time.Duration) string {
		bar := m.FooterView(Info{Notice: &Notice{
			Kind:  styles.NoticeOkay,
			Body:  body,
			Since: base,
			Now:   base.Add(NoticeDuration + left),
			Left:  base.Add(NoticeDuration),
		}})
		plain := ansi.Strip(bar)
		plain = plain[strings.Index(plain, "OKAY!")+len("OKAY!"):]
		return strings.TrimSpace(plain)
	}
	want := []rune(body)
	sameAt := func(text string) int {
		got := []rune(text)
		n := 0
		for i := 0; i < len(want) && i < len(got); i++ {
			if got[i] == want[i] {
				n++
			}
		}
		return n
	}
	if got := sameAt(notice(0)); got != len(want) {
		t.Fatalf("leaving notice settled %d/%d at the start", got, len(want))
	}
	mid := sameAt(notice(NoticeDuration / 2))
	if mid == 0 || mid >= len(want) {
		t.Fatalf("halfway out, %d/%d characters were still themselves", mid, len(want))
	}
	if got := sameAt(notice(NoticeDuration)); got != 0 {
		t.Fatalf("notice did not fully dissolve: %d/%d", got, len(want))
	}
}

// TestScrambleGlyphsStayInOneCell: the soup must not shift the bar, and it has
// to cover the mixed case, digits and symbols the effect is made of.
func TestScrambleGlyphsStayInOneCell(t *testing.T) {
	have := make(map[rune]bool, len(scrambleGlyphs))
	for _, r := range scrambleGlyphs {
		have[r] = true
		if w := ansi.StringWidth(string(r)); w != 1 {
			t.Fatalf("glyph %q is %d cells wide, want 1", r, w)
		}
	}
	for _, r := range "0D€b*@$3C£#7!Dλ♥→✓∞⌘♠ñ" {
		if !have[r] {
			t.Fatalf("scramble soup is missing %q", r)
		}
	}
}

// TestNoticeWordNamesTheKind: each colour family leads with its own word.
func TestNoticeWordNamesTheKind(t *testing.T) {
	for kind, want := range map[styles.NoticeKind]string{
		styles.NoticeOkay: "OKAY!",
		styles.NoticeInfo: "FYI",
		styles.NoticeWarn: "HMM!",
		styles.NoticeFail: "OOPS!",
	} {
		bar := testFooter(Notice{Kind: kind, Body: "x", Since: time.Unix(1700000000, 0), Now: time.Unix(1700000000, 0)})
		if !strings.Contains(ansi.Strip(bar), want) {
			t.Fatalf("kind %v did not lead with %q: %q", kind, want, ansi.Strip(bar))
		}
	}
}
