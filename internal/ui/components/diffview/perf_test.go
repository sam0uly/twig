package diffview

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

// largeFile builds a diff with many thousands of rows so the paint path is
// exercised at a realistic scale. Every line differs, so the diff carries a
// row for each side rather than a handful of context windows.
func largeFile(lines int) *diff.File {
	var oldB, newB strings.Builder
	for i := range lines {
		oldB.WriteString("func handler")
		oldB.WriteString(strings.Repeat("x", i%9))
		oldB.WriteString("() error { return nil }\n")
		newB.WriteString("func handler")
		newB.WriteString(strings.Repeat("x", i%9))
		newB.WriteString("() error { return err }\n")
	}
	return diff.Generate("big.go", diff.Modified, oldB.String(), newB.String(), diff.GenerateOptions{Context: 3})
}

func BenchmarkViewLargeDiff(b *testing.B) {
	f := largeFile(20000)
	m := New(false, styles.New())
	m.SetSize(120, 40)
	m.SetFile(f)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.vp.SetYOffset((i * 997) % len(m.rows))
		_ = m.View()
	}
}

// TestViewIsIndependentOfFileSize makes sure painting only styles the rows in
// view: a huge diff must render far fewer rows than it contains.
func TestViewIsIndependentOfFileSize(t *testing.T) {
	f := largeFile(20000)
	m := New(false, styles.New())
	m.SetSize(120, 40)
	m.SetFile(f)

	if len(m.rows) < 10000 {
		t.Fatalf("expected a large diff, got %d rows", len(m.rows))
	}
	if m.rendered != nil {
		t.Fatal("non-wrapped mode should not pre-render every row")
	}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 40 {
		t.Fatalf("View painted %d lines, want 40", len(lines))
	}
}

// BenchmarkViewScrollLine measures the steady-state scroll path: one new row
// per frame, the rest served from the rendered-row cache.
func BenchmarkViewScrollLine(b *testing.B) {
	f := largeFile(20000)
	m := New(false, styles.New())
	m.SetSize(120, 40)
	m.SetFile(f)
	span := max(1, len(m.rows)-41)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.vp.SetYOffset(i % span)
		_ = m.View()
	}
}

// TestRenderedRowCache checks that repainting an unmoved window reuses the
// styled rows instead of styling them again.
func TestRenderedRowCache(t *testing.T) {
	f := largeFile(200)
	m := New(false, styles.New())
	m.SetSize(120, 40)
	m.SetFile(f)

	m.View()
	if got := len(m.cache.rows); got < 30 {
		t.Fatalf("cached %d rows after a paint, want the visible window", got)
	}

	before := m.cache.rows[0]
	m.View()
	if after := m.cache.rows[0]; after != before {
		t.Fatal("repaint should reuse the cached row")
	}

	// Any mode change must drop the cache; only rows painted since count.
	m.SetFile(f)
	if len(m.cache.rows) != 0 {
		t.Fatalf("SetFile kept %d stale rows", len(m.cache.rows))
	}
}
