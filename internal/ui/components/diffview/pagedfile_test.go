package diffview

import (
	"fmt"
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

// changedFile builds a diff with many small hunks spread over the document,
// which is what a real large change looks like: context lines break it into
// hunks that fall on either side of a block boundary.
func changedFile(n, every int) *diff.File {
	var oldText, newText strings.Builder
	for i := 0; i < n; i++ {
		if i%every == every/2 {
			fmt.Fprintf(&oldText, "MARKER_%d before\n", i)
			fmt.Fprintf(&newText, "MARKER_%d after\n", i)
			continue
		}
		fmt.Fprintf(&oldText, "line %d padding padding\n", i)
		fmt.Fprintf(&newText, "line %d padding padding\n", i)
	}
	return diff.Generate("wide.go", diff.Modified, oldText.String(), newText.String(),
		diff.GenerateOptions{Context: 3})
}

// TestPagedFileSplitParity renders a paged diff both ways in split mode: the
// page rebuild must not change a single row.
func TestPagedFileSplitParity(t *testing.T) {
	f := changedFile(20_000, 10)
	if !f.Paged() {
		t.Fatal("the fixture must be paged")
	}

	flat := newFlatView(f, 120, 24)
	virt := newVirtualView(f, 120, 24)
	flat.SetSplit(true)
	virt.SetSplit(true)

	if got, want := virt.nrows(), flat.nrows(); got != want {
		t.Fatalf("split rows = %d, flat = %d", got, want)
	}
	for _, off := range []int{0, 700, flat.nrows() / 2, flat.nrows() - 30} {
		virt.JumpTo(off)
		flat.JumpTo(off)
		if a, b := virt.View(), flat.View(); a != b {
			t.Fatalf("split view at %d differs:\nvirtual:\n%s\nflat:\n%s", off, a, b)
		}
	}
}

// TestPagedFileBlocks checks the all-changes document over a paged diff: its
// file block must lay out and paint the same rows as the aggregated resident
// equivalent. The aggregate has no notes or file header, so the rows are
// compared directly instead of through a flat view.
func TestPagedFileBlocks(t *testing.T) {
	f := changedFile(20_000, 10)
	if !f.Paged() {
		t.Fatal("the fixture must be paged")
	}

	want := diff.Aggregate([]*diff.File{f, f})
	if want.Paged() {
		t.Fatal("the aggregate should be resident")
	}

	m := New(false, styles.New())
	m.SetSize(120, 24)
	m.SetDocument(func(i int) *diff.File { return f })
	m.AppendBlocks([]*diff.File{f, f})

	// Every line of both files, each block starting with its band row.
	if got, exp := m.nrows(), want.NLines(); got != exp {
		t.Fatalf("rows = %d, want %d", got, exp)
	}

	for i := 0; i < m.nrows(); i++ {
		got, exp := m.rowAt(i).line, want.Lines[i]
		if got.Kind != exp.Kind || got.Text != exp.Text || got.Old != exp.Old || got.New != exp.New {
			t.Fatalf("row %d: got %+v, want %+v", i, got, exp)
		}
		if text := m.textAt(i); text != exp.Text {
			t.Fatalf("row %d text = %q, want %q", i, text, exp.Text)
		}
	}

	// The band of the first block must offer a patch, which forces the paged
	// hunk bodies to rebuild through the block.
	m.JumpTo(0)
	if patch := m.CurrentFilePatch(); !strings.Contains(patch, "@@") {
		t.Fatalf("file patch = %q", patch)
	}
}
