package diffview

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/ui/styles"
)

// wheelTick is what the viewport reports for one notch of the wheel, which is the
// distance smallStepRows is measured against.
const wheelTick = 3

// TestWheelTickLandsAtOnce: a wheel tick is three rows, and three rows is the short
// step that lands at once. Animating it puts the window a frame or two behind the
// wheel, which is the hijacked scroll the setting is supposed to avoid.
func TestWheelTickLandsAtOnce(t *testing.T) {
	m := New(true, styles.New())
	m.SetSize(100, 20)
	m.SetFile(bigFile(400))
	if m.nrows() < 3*m.vpHeight() {
		t.Fatalf("the fixture has %d rows, too few to scroll", m.nrows())
	}
	m.vp.SetYOffset(10)
	before := m.YOffset()

	next, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	m = next
	if m.anim {
		t.Fatal("a wheel tick started a glide")
	}
	if got, want := m.YOffset(), before+wheelTick; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

// TestPageStepStillGlides: the short step that lands is the wheel and the line, not a
// page. A page move is worth animating, or the jump loses the reader's place.
func TestPageStepStillGlides(t *testing.T) {
	m := New(true, styles.New())
	m.SetSize(100, 20)
	m.SetFile(bigFile(400))
	m.vp.SetYOffset(10)

	if cmd := m.ScrollTo(m.YOffset() + m.vpHeight()); cmd == nil {
		t.Fatal("a page step returned no frame command")
	}
	if !m.anim {
		t.Fatal("a page step landed instead of gliding")
	}
}

// TestShortScrollToLands: a target a few rows away arrives at once, since the spring
// would spend its first frames covering a distance nobody can see.
func TestShortScrollToLands(t *testing.T) {
	m := New(true, styles.New())
	m.SetSize(100, 20)
	m.SetFile(bigFile(400))
	m.vp.SetYOffset(10)

	if cmd := m.ScrollTo(m.YOffset() + wheelTick); cmd != nil {
		t.Fatal("a three row scroll scheduled a frame")
	}
	if m.anim {
		t.Fatal("a three row scroll glided")
	}
	if got, want := m.YOffset(), 10+wheelTick; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}
