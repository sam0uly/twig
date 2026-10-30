package ui

import (
	"testing"

	"charm.land/tone"
)

// TestNewestWelcomeRowEmphasis covers what the bottom row used to get wrong:
// it brightened the hash while dimming the subject, which read as an inverted
// emphasis, and its dot pulsed to near-white, which looked like a different
// kind of dot rather than the same dot moving.
func TestNewestWelcomeRowEmphasis(t *testing.T) {
	normal := welcomeRowStylesFor(false, 0)
	newest := welcomeRowStylesFor(true, 0)
	pulsing := welcomeRowStylesFor(true, 6)

	// one palette for every row: a hash identifies a commit and a subject
	// describes it, so the newest row does not reweight either
	if normal.subject.GetForeground() != newest.subject.GetForeground() {
		t.Fatalf("newest subject = %v, want the same as a normal row",
			newest.subject.GetForeground())
	}
	if newest.subject.GetBold() || normal.subject.GetBold() {
		t.Fatal("the subject is not bold on any row")
	}
	if normal.hash.GetForeground() != newest.hash.GetForeground() {
		t.Fatalf("newest hash = %v, want the same as a normal row",
			newest.hash.GetForeground())
	}

	// the dot is what marks the newest row, and it stays in the accent family
	for _, palette := range []welcomeRowStyles{normal, newest, pulsing} {
		switch got := palette.dot.GetForeground(); got {
		case tone.Blush, tone.Dolly:
		default:
			t.Fatalf("dot foreground = %v, want an accent tone", got)
		}
	}
	if newest.dot.GetForeground() == pulsing.dot.GetForeground() {
		t.Fatal("the newest dot does not pulse")
	}
}
