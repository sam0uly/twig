package diffview

import (
	"fmt"
	"strings"
	"testing"

	"twig/internal/diff"
	"twig/internal/ui/styles"
)

// bigFile builds a diff of n added lines with a few marker lines so search and
// hunk navigation have something to find.
func bigFile(n int) *diff.File {
	var oldText, newText strings.Builder
	oldText.WriteString("base\n")
	for i := 0; i < n; i++ {
		if i%500 == 250 {
			fmt.Fprintf(&newText, "MARKER_%d key\n", i)
			continue
		}
		fmt.Fprintf(&newText, "line %d padding padding\n", i)
	}
	return diff.Generate("huge.go", diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3})
}

func newFlatView(f *diff.File, w, h int) Model {
	m := New(false, styles.New())
	m.SetSize(w, h)
	m.setFlat(f)
	return m
}

func newVirtualView(f *diff.File, w, h int) Model {
	m := New(false, styles.New())
	m.SetSize(w, h)
	m.SetLines(f)
	return m
}

// TestLineDocMatchesFlatLayout is the safety net for the virtual single-file
// layout: it must produce the same rows, hunks and bands as the flat path.
func TestLineDocMatchesFlatLayout(t *testing.T) {
	f := bigFile(20_000)
	if f.NLines() > hugeFileLines {
		t.Fatalf("fixture has %d lines, expected to stay under the auto-switch", f.NLines())
	}
	if !f.Paged() {
		t.Fatal("the fixture must be paged, or the page rebuild is untested here")
	}

	flat := newFlatView(f, 100, 20)
	virt := newVirtualView(f, 100, 20)

	if got, want := virt.nrows(), flat.nrows(); got != want {
		t.Fatalf("virtual rows = %d, flat rows = %d", got, want)
	}
	if got, want := virt.nlines(), flat.nlines(); got != want {
		t.Fatalf("virtual lines = %d, flat lines = %d", got, want)
	}
	if len(virt.hunks) != len(flat.hunks) {
		t.Fatalf("virtual hunks = %d, flat hunks = %d", len(virt.hunks), len(flat.hunks))
	}
	for i := range flat.hunks {
		if virt.hunks[i] != flat.hunks[i] {
			t.Fatalf("hunk %d at %d, flat at %d", i, virt.hunks[i], flat.hunks[i])
		}
	}

	// Spot-check rows across the document, including block boundaries.
	checks := []int{0, 1, 63, docBlockLines - 1, docBlockLines, docBlockLines + 1, flat.nrows() - 1}
	for _, i := range checks {
		if i < 0 || i >= flat.nrows() {
			continue
		}
		gotLine, gotOK := virt.lineForRow(i)
		wantLine, wantOK := flat.lineForRow(i)
		if gotOK != wantOK {
			t.Fatalf("row %d: virtual ok=%v, flat ok=%v", i, gotOK, wantOK)
		}
		if gotLine.Text != wantLine.Text || gotLine.Kind != wantLine.Kind ||
			gotLine.Old != wantLine.Old || gotLine.New != wantLine.New {
			t.Fatalf("row %d: virtual %+v, flat %+v", i, gotLine, wantLine)
		}
		if got := virt.textAt(i); got != flat.textAt(i) {
			t.Fatalf("line %d: virtual text %q, flat %q", i, got, flat.textAt(i))
		}
	}

	// The window must paint identically.
	virt.JumpTo(0)
	flat.JumpTo(0)
	if a, b := virt.View(), flat.View(); a != b {
		t.Fatalf("top of the document differs:\nvirtual:\n%s\nflat:\n%s", a, b)
	}
	virt.JumpTo(1000)
	flat.JumpTo(1000)
	if a, b := virt.View(), flat.View(); a != b {
		t.Fatalf("row 1000 differs:\nvirtual:\n%s\nflat:\n%s", a, b)
	}
}

// TestSetFileSwitchesToVirtualLayout checks the automatic hand-over.
func TestSetFileSwitchesToVirtualLayout(t *testing.T) {
	f := bigFile(1200)
	m := New(false, styles.New())
	m.SetSize(100, 20)

	m.SetFile(f) // under the threshold: flat
	if m.doc != nil {
		t.Fatal("a small diff should stay on the flat layout")
	}

	m.SetLines(f) // force the virtual layout for the same file
	if m.doc == nil || !m.doc.single() {
		t.Fatal("SetLines should build a single-file virtual document")
	}
	if m.File() != f {
		t.Fatal("the virtual layout should keep the file")
	}
}

func TestLineDocEvictsRows(t *testing.T) {
	f := bigFile(20000) // ~10 blocks
	m := newVirtualView(f, 100, 20)

	blocks, materialised := m.DocBlocks()
	if blocks < 5 {
		t.Fatalf("blocks = %d, want several", blocks)
	}
	if materialised > docWindow {
		t.Fatalf("materialised %d blocks, want at most %d", materialised, docWindow)
	}

	// Walk to the far end and back; the row count must stay stable.
	rows := m.nrows()
	m.JumpTo(rows - 30)
	_ = m.View()
	if _, materialised := m.DocBlocks(); materialised > docWindow {
		t.Fatalf("materialised %d blocks after the jump", materialised)
	}
	m.JumpTo(0)
	_ = m.View()
	if got := m.nrows(); got != rows {
		t.Fatalf("rows changed from %d to %d", rows, got)
	}
	if _, materialised := m.DocBlocks(); materialised > docWindow {
		t.Fatalf("materialised %d blocks after scrolling back", materialised)
	}
}

func TestLineDocSearchAcrossBlocks(t *testing.T) {
	f := bigFile(20000)
	m := newVirtualView(f, 100, 20)

	n, cmd := m.Search("MARKER_17750")
	if n != 0 {
		t.Fatalf("phase one found %d matches, want none far below", n)
	}
	if cmd == nil {
		t.Fatal("the background scan should be scheduled")
	}
	msg, ok := cmd().(SearchResultsMsg)
	if !ok {
		t.Fatalf("background scan returned %T", cmd())
	}
	if len(msg.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(msg.Matches))
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}

	sel := m.matches[m.matchIdx]
	row := m.rowAt(m.rowForLine(sel.Line))
	if !strings.Contains(row.line.Text, "MARKER_17750") {
		t.Fatalf("match landed on %q", row.line.Text)
	}
	if m.YOffset() == 0 {
		t.Fatal("applying the results should scroll to the match")
	}
	_ = m.View()
}

func TestLineDocSplitMode(t *testing.T) {
	f := bigFile(5000)
	m := newVirtualView(f, 100, 20)
	unified := m.nrows()

	m.SetSplit(true)
	if got := m.nrows(); got == 0 || got == unified {
		t.Fatalf("split rows = %d, unified = %d", got, unified)
	}
	if len(m.hunks) == 0 {
		t.Fatal("split mode lost the hunks")
	}
	m.JumpTo(500)
	_ = m.View()

	m.SetSplit(false)
	if got := m.nrows(); got != unified {
		t.Fatalf("rows after toggling back = %d, want %d", got, unified)
	}
}

func TestLineDocGutterAndPatch(t *testing.T) {
	f := bigFile(4000)
	m := newVirtualView(f, 100, 20)

	// The gutter must show the same numbers the flat layout would.
	flat := newFlatView(f, 100, 20)
	for _, row := range []int{2, 40, docBlockLines + 5} {
		got, ok := m.lineForRow(row)
		want, wantOK := flat.lineForRow(row)
		if ok != wantOK || got.Old != want.Old || got.New != want.New {
			t.Fatalf("row %d gutter: virtual %+v/%v, flat %+v/%v", row, got, ok, want, wantOK)
		}
	}

	if patch := m.CurrentFilePatch(); !strings.Contains(patch, "@@") {
		t.Fatalf("file patch = %q", patch)
	}

	// The hunk in view is only defined once a hunk header is on screen.
	if len(m.hunks) == 0 {
		t.Fatal("the virtual document has no hunks")
	}
	m.JumpTo(m.hunks[0])
	if patch := m.CurrentHunkPatch(); !strings.Contains(patch, "@@") {
		t.Fatalf("hunk patch = %q", patch)
	}
}

func TestBlockMetricsMatchRows(t *testing.T) {
	// A resident file and a paged one: the layout path must agree with the
	// rows it paints in both representations.
	for _, f := range []*diff.File{bigFile(3000), bigFile(20_000)} {
		doc := newLineDoc(f, nil, nil, 128)
		for i := 0; i < doc.nblocks(); i++ {
			for _, split := range []bool{false, true} {
				doc.setMode(split)
				rows := docRows(doc.blockLines(&doc.blocks[i]), 0, split, nil)

				mode := modeUnified
				if split {
					mode = modeSplit
				}
				if got, want := doc.blocks[i].rows[mode], len(rows); got != want {
					t.Fatalf("block %d split=%v: recorded %d rows, built %d", i, split, got, want)
				}
				var hunks []int
				for j, r := range rows {
					if r.hunk {
						hunks = append(hunks, j)
					}
				}
				if fmt.Sprint(hunks) != fmt.Sprint(doc.blocks[i].hunks[mode]) {
					t.Fatalf("block %d split=%v: recorded hunks %v, built %v",
						i, split, doc.blocks[i].hunks[mode], hunks)
				}
			}
		}
	}
}

// TestHugeFileSearchCount searches a document large enough to use the virtual
// layout for a term that matches most lines: the count must be complete and the
// scan must not fall over on millions of hits.
func TestHugeFileSearchCount(t *testing.T) {
	const lines = 60_000
	f := bigFile(lines)
	m := newVirtualView(f, 100, 20)

	if m.doc == nil {
		t.Fatal("a 60k line diff should use the virtual layout")
	}

	_, cmd := m.Search("key")
	if cmd == nil {
		t.Fatal("the background scan should be scheduled")
	}
	msg, ok := cmd().(SearchResultsMsg)
	if !ok {
		t.Fatalf("background scan returned %T", cmd())
	}
	// Every 500th line carries a marker with "key"; the rest do not.
	want := lines / 500
	if len(msg.Matches) != want {
		t.Fatalf("matches = %d, want %d", len(msg.Matches), want)
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}
	if m.MatchCount() != want {
		t.Fatalf("count = %d, want %d", m.MatchCount(), want)
	}

	// Jump between hits: each must land on a row that contains the term.
	for _, idx := range []int{0, want / 2, want - 1} {
		sel := m.matches[idx]
		row := m.rowAt(m.rowForLine(sel.Line))
		if !strings.Contains(strings.ToLower(row.line.Text), "key") {
			t.Fatalf("match %d landed on %q", idx, row.line.Text)
		}
	}
}
