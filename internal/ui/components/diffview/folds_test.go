package diffview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

func TestFoldTogglesCommitMetadata(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(60, 20)
	m.SetNotes([]string{"hash sam", "subject", "body", "2 files changed"})
	m.SetFile(sampleFile())
	before := m.nrows()

	m.ToggleFold()
	if got := m.nrows(); got != before-3 {
		t.Fatalf("folded notes rows = %d, want %d", got, before-3)
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "body") {
		t.Fatalf("folded notes remain visible:\n%s", view)
	}
	view := m.View()
	if got := strings.Count(view, foldOpenMarker+" "); got != 1 {
		t.Fatalf("folded metadata open markers = %d, want 1:\n%s", got, view)
	}
	if got := strings.Count(view, foldClosedMarker+" "); got != 1 {
		t.Fatalf("unfolded hunk markers = %d, want 1:\n%s", got, view)
	}

	m.ToggleFold()
	if got := m.nrows(); got != before {
		t.Fatalf("unfolded notes rows = %d, want %d", got, before)
	}
}

func TestStagingRailSurvivesFolding(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(80, 20)
	m.SetSelectable(true)
	m.SetNotes([]string{"hash sam", "subject", "body"})
	m.SetFile(sampleFile())

	m.ToggleFold()
	logical := m.hunks[0] + 1
	header := m.displayOfLogical(logical - 1)
	body := m.displayOfLogical(logical)
	if got, want := m.barAt(header), m.styles.HunkEdge().Render("│"); got != want {
		t.Fatalf("folded hunk header rail = %q, want %q", got, want)
	}
	if got, want := m.barAt(body), m.styles.HunkEdge().Render("│"); got != want {
		t.Fatalf("folded hunk body rail = %q, want %q", got, want)
	}

	m.ToggleFold()
	body = m.displayOfLogical(logical)
	if got, want := m.barAt(body), m.styles.HunkEdge().Render("│"); got != want {
		t.Fatalf("unfolded hunk body rail = %q, want %q", got, want)
	}
}

func TestFoldTogglesHunk(t *testing.T) {
	m := newSized(false, 80)
	m.SetSelectable(true)
	row := m.hunks[0]
	if !m.CursorTo(row) {
		t.Fatal("could not place the cursor on the hunk")
	}
	before := m.nrows()

	m.ToggleFold()
	if got := m.nrows(); got >= before {
		t.Fatalf("folded hunk rows = %d, want fewer than %d", got, before)
	}
	if m.rowAt(m.cursor).line.Kind != diff.HunkHeader {
		t.Fatal("folded hunk did not keep its header")
	}
	if !strings.Contains(m.View(), foldOpenMarker+" ") {
		t.Fatalf("folded hunk did not show its marker:\n%s", m.View())
	}

	m.ToggleFold()
	if got := m.nrows(); got != before {
		t.Fatalf("unfolded hunk rows = %d, want %d", got, before)
	}
}

func TestFoldTogglesFile(t *testing.T) {
	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "one\n", "one changed\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("b.go", diff.Modified, "two\n", "two changed\n", diff.GenerateOptions{Context: 3}),
	}
	m := New(false, styles.New())
	m.SetSize(100, 30)
	m.SetSelectable(true)
	m.SetFile(diff.Aggregate(files))
	before := m.nrows()
	if !m.CursorTo(m.fileBands[0]) {
		t.Fatal("could not place the cursor on the file band")
	}

	m.ToggleFold()
	if got := m.nrows(); got >= before {
		t.Fatalf("folded file rows = %d, want fewer than %d", got, before)
	}
	if got := m.rowAt(m.cursor).line.Text; !strings.HasPrefix(got, "── a.go") {
		t.Fatalf("folded file kept %q, want its band", got)
	}
	if !strings.Contains(m.View(), foldOpenMarker+" ") {
		t.Fatalf("folded file did not show its marker:\n%s", m.View())
	}

	m.ToggleFold()
	if got := m.nrows(); got != before {
		t.Fatalf("unfolded file rows = %d, want %d", got, before)
	}
}
