package diffview

import (
	"fmt"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

// blockFile is a small two-hunk diff, the shape an all-changes block has.
func blockFile(i int) *diff.File {
	return diff.Generate(fmt.Sprintf("f%d.go", i), diff.Modified,
		"a\nb\nc\nd\ne\n", "a\nB\nc\nd\nE\n", diff.GenerateOptions{Context: 3})
}

// appendBlocks streams n blocks in chunks, the way the all-changes view does.
func appendBlocks(m *Model, n, chunk int) {
	for i := 0; i < n; i += chunk {
		batch := make([]*diff.File, min(chunk, n-i))
		for j := range batch {
			batch[j] = blockFile(i + j)
		}
		m.AppendBlocks(batch)
	}
}

// TestAppendKeepsTheIndexHonest: streaming blocks in must leave the row index
// agreeing with the layout, however many times it had to grow.
func TestAppendKeepsTheIndexHonest(t *testing.T) {
	m := New(false, styles.New())
	m.SetSize(120, 40)
	m.SetDocument(func(int) *diff.File { return nil })
	appendBlocks(&m, 3000, 24)

	blocks := m.doc.nblocks()
	if blocks != 3000 {
		t.Fatalf("%d blocks, want 3000", blocks)
	}
	if got := m.doc.idx[modeUnified].count(); got != m.doc.idx[modeUnified].prefix(blocks) {
		t.Fatalf("the index reports %d rows, its prefix walk says %d", got, m.doc.idx[modeUnified].prefix(blocks))
	}
	if len(m.fileBands) != blocks {
		t.Fatalf("%d bands for %d blocks", len(m.fileBands), blocks)
	}
	// Every block starts where the nav says it does, and the bands agree.
	for i := 0; i < blocks; i += 137 {
		start, _ := m.doc.layout(i)
		if start < 0 || start >= m.nrows() {
			t.Fatalf("block %d starts at %d, outside %d rows", i, start, m.nrows())
		}
		if m.fileBands[i] != start {
			t.Fatalf("band %d is at %d, layout says %d", i, m.fileBands[i], start)
		}
	}
}

// BenchmarkAppendBlocks watches the cost of opening the all-changes view on a
// big repository. It used to be quadratic: the row index rebuilt itself on every
// block, and the nav walked every block already recorded.
func BenchmarkAppendBlocks(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("blocks=%d", n), func(b *testing.B) {
			for b.Loop() {
				m := New(false, styles.New())
				m.SetSize(120, 40)
				m.SetDocument(func(int) *diff.File { return nil })
				appendBlocks(&m, n, 24)
			}
		})
	}
}

// TestRowIndexStaysExact pins the prefix sums themselves: a Fenwick tree that
// grows has to keep every slot its updates walk through, or the counts go short.
func TestRowIndexStaysExact(t *testing.T) {
	var r rowIndex
	total := 0
	for i := 0; i < 500; i++ {
		r.append(7)
		total += 7
		if got, want := r.count(), total; got != want {
			t.Fatalf("after %d blocks: count = %d, want %d", i+1, got, want)
		}
		if got, want := r.prefix(i), total-7; got != want {
			t.Fatalf("after %d blocks: prefix(%d) = %d, want %d", i+1, i, got, want)
		}
	}
}
