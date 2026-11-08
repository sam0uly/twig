package diffview

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

// twoHunkFile has two changes far enough apart to make two hunks.
func twoHunkFile() *diff.File {
	var old, next strings.Builder
	for i := 1; i <= 40; i++ {
		line := "line\n"
		if i == 3 {
			old.WriteString("before\n")
			next.WriteString("after\n")
			continue
		}
		if i == 30 {
			old.WriteString("earlier\n")
			next.WriteString("later\n")
			continue
		}
		old.WriteString(line)
		next.WriteString(line)
	}
	return diff.Generate("a.go", diff.Modified, old.String(), next.String(), diff.GenerateOptions{Context: 3})
}

func readRailSized(t *testing.T, f *diff.File, notes []string, height int) Model {
	t.Helper()
	m := New(false, styles.New())
	m.SetSize(60, height)
	if len(notes) > 0 {
		m.SetNotes(notes)
	}
	m.SetFile(f)
	m.SetReadRail(true)
	return m
}

// TestReadRailFollowsTheView: the rail marks the section that owns the top of
// the window, so every normal scroll carries it along.
func TestReadRailFollowsTheView(t *testing.T) {
	m := readRailSized(t, twoHunkFile(), []string{"commit metadata", "subject"}, 8)
	if len(m.hunks) < 2 {
		t.Fatalf("wanted two hunks, got %d", len(m.hunks))
	}
	hunk0, hunk1 := m.hunks[0], m.hunks[1]
	if got := m.ReadRow(); got != 0 {
		t.Fatalf("rail rests on row %d, want the commit metadata at 0", got)
	}

	m.SetScroll(m.displayIndex(hunk0))
	if got := m.ReadRow(); got != hunk0 {
		t.Fatalf("rail = %d at the first hunk, want %d", got, hunk0)
	}

	// Inside the body the header is above the view, but the section still fills
	// the screen: the rail holds it and paints on the top row.
	m.SetScroll(m.displayIndex(hunk0) + 2)
	if got := m.ReadRow(); got != hunk0 {
		t.Fatalf("rail moved to %d inside the first hunk, want to hold %d", got, hunk0)
	}
	if got, want := m.barAt(m.vp.YOffset()), m.styles.CursorBar().Render("┃"); got != want {
		t.Fatalf("scrolled rail = %q, want the mark on the top row", got)
	}

	// The next title reaches the top: now the rail jumps.
	m.SetScroll(m.displayIndex(hunk1))
	if got := m.ReadRow(); got != hunk1 {
		t.Fatalf("rail = %d at the second hunk, want %d", got, hunk1)
	}
	if got := strings.Count(m.View(), m.styles.CursorBar().Render("┃")); got != 1 {
		t.Fatalf("painted %d trails, want 1", got)
	}
}

// TestFoldFollowsTheRail: `o` folds the section the rail is on, which is the one
// owning the top of the window.
func TestFoldFollowsTheRail(t *testing.T) {
	m := readRailSized(t, twoHunkFile(), nil, 8)
	if len(m.hunks) < 2 {
		t.Fatalf("wanted two hunks, got %d", len(m.hunks))
	}
	m.SetScroll(m.displayIndex(m.hunks[1]))
	if got, want := m.ReadRow(), m.hunks[1]; got != want {
		t.Fatalf("rail rests on %d, want the second hunk %d", got, want)
	}
	before := m.nrows()
	m.ToggleFold()
	if !m.folds["hunk:1"] {
		t.Fatal("fold did not land on the hunk the rail marks")
	}
	if m.folds["hunk:0"] {
		t.Fatal("fold landed on the first hunk, not the rail's")
	}
	if got := m.nrows(); got >= before {
		t.Fatalf("folded rows = %d, want fewer than %d", got, before)
	}
}
