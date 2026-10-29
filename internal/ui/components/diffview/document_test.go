package diffview

import (
	"fmt"
	"strings"
	"testing"

	"twig/internal/diff"
	"twig/internal/ui/styles"
)

// docTestFile builds a diff with a single changed line in the middle.
func docTestFile(path string, lines int, marker string) *diff.File {
	var oldText, newText strings.Builder
	for i := 0; i < lines; i++ {
		oldText.WriteString("line\n")
		if i == lines/2 {
			newText.WriteString(marker + "\n")
			continue
		}
		newText.WriteString("line\n")
	}
	return diff.Generate(path, diff.Modified, oldText.String(), newText.String(), diff.GenerateOptions{Context: 3})
}

// newDocView builds a virtual document of n blocks, counting materialisations.
func newDocView(t *testing.T, files []*diff.File, loads *int) Model {
	t.Helper()
	m := New(false, styles.New())
	m.SetSize(80, 10)
	m.SetDocument(func(i int) *diff.File {
		*loads++
		return files[i]
	})
	m.AppendBlocks(files)
	return m
}

func TestDocumentLayoutAndEviction(t *testing.T) {
	const n = 12
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.go", i), 20, "changed")
	}
	loads := 0
	m := newDocView(t, files, &loads)

	blocks, materialised := m.DocBlocks()
	if blocks != n {
		t.Fatalf("blocks = %d, want %d", blocks, n)
	}
	if materialised > docWindow {
		t.Fatalf("materialised %d blocks, want at most %d", materialised, docWindow)
	}

	// Unified rows are one per document line: a separator plus the file lines.
	want := 0
	for _, f := range files {
		want += 1 + len(f.Lines)
	}
	if got := m.nrows(); got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if len(m.fileBands) != n {
		t.Fatalf("bands = %d, want %d", len(m.fileBands), n)
	}
	for i := 1; i < len(m.fileBands); i++ {
		if m.fileBands[i] <= m.fileBands[i-1] {
			t.Fatalf("band %d (%d) not after band %d (%d)", i, m.fileBands[i], i-1, m.fileBands[i-1])
		}
	}

	// The first blocks were evicted when the document grew; reading one must
	// materialise it again from the loader rather than paint blank rows.
	loads = 0
	row := m.rowAt(m.fileBands[0])
	if !strings.Contains(row.line.Text, "f0.go") {
		t.Fatalf("band row = %q, want the first file's separator", row.line.Text)
	}
	if loads == 0 {
		t.Fatal("an evicted block should be materialised on demand")
	}
	if _, materialised := m.DocBlocks(); materialised > docWindow {
		t.Fatalf("materialised %d blocks after the read, want at most %d", materialised, docWindow)
	}

	// Jumping to a far block and painting its window must show its separator.
	m.JumpToBandNow(n - 1)
	if out := m.View(); !strings.Contains(out, fmt.Sprintf("f%d.go", n-1)) {
		t.Fatalf("view does not show the block separator:\n%s", out)
	}
}

func TestDocumentSearchAcrossBlocks(t *testing.T) {
	const n = 30
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.go", i), 20, "changed")
	}
	files[n-1] = docTestFile("last.go", 20, "MAGIC")

	loads := 0
	m := newDocView(t, files, &loads)

	windowMatches, cmd := m.Search("MAGIC")
	if windowMatches != 0 {
		t.Fatalf("phase one found %d matches, want none: the hit is far below", windowMatches)
	}
	if cmd == nil {
		t.Fatal("the background scan should be scheduled")
	}
	msg, ok := cmd().(SearchResultsMsg)
	if !ok {
		t.Fatalf("got %T, want SearchResultsMsg", cmd())
	}
	if len(msg.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(msg.Matches))
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}
	if m.MatchCount() != 1 {
		t.Fatalf("match count = %d, want 1", m.MatchCount())
	}
	// The jump must land somewhere inside the last block.
	if m.YOffset() < m.fileBands[n-1] {
		t.Fatalf("view at %d, want at least the last band %d", m.YOffset(), m.fileBands[n-1])
	}
}

func TestDocumentSplitMode(t *testing.T) {
	const n = 6
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.go", i), 20, "changed")
	}
	loads := 0
	m := newDocView(t, files, &loads)

	unified := m.nrows()

	m.SetSplit(true)
	split := m.nrows()
	if split == 0 {
		t.Fatal("split mode produced no rows")
	}
	if split >= unified {
		t.Fatalf("split rows = %d, want fewer than unified %d (changed lines pair up)", split, unified)
	}
	if len(m.fileBands) != n {
		t.Fatalf("bands = %d, want %d", len(m.fileBands), n)
	}
	for i := 1; i < len(m.fileBands); i++ {
		if m.fileBands[i] <= m.fileBands[i-1] {
			t.Fatal("bands are not increasing in split mode")
		}
	}
	m.JumpToBandNow(n - 1)
	if m.YOffset() == 0 {
		t.Fatal("jumping in split mode did not move the view")
	}

	m.SetSplit(false)
	if got := m.nrows(); got != unified {
		t.Fatalf("rows after toggling back = %d, want %d", got, unified)
	}
}

func TestDocumentCurrentHunkPatch(t *testing.T) {
	const n = 10
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.go", i), 20, "changed")
	}
	loads := 0
	m := newDocView(t, files, &loads)

	m.JumpToBandNow(n - 1)
	patch := m.CurrentHunkPatch()
	if !strings.Contains(patch, "@@") {
		t.Fatalf("patch = %q, want a hunk header from the last block", patch)
	}
	if !strings.Contains(patch, "changed") {
		t.Fatalf("patch = %q, want the changed line", patch)
	}
}

func TestDocumentHasBandAndBands(t *testing.T) {
	files := []*diff.File{docTestFile("a.go", 10, "x"), docTestFile("b.go", 10, "y")}
	loads := 0
	m := newDocView(t, files, &loads)

	if !m.HasBand(0) || !m.HasBand(1) {
		t.Fatal("both blocks should have bands")
	}
	if m.HasBand(2) || m.HasBand(-1) {
		t.Fatal("bands outside the document should not exist")
	}
	if got := len(m.fileBands); got != 2 {
		t.Fatalf("bands = %d, want 2", got)
	}
	// rowAt beyond the document must stay safe.
	_ = m.rowAt(m.nrows() + 100)
}

// TestDocumentConcurrentScanAndAppend runs a background scan while the
// document keeps receiving blocks: the layout may only be read under the
// document's lock, which the race detector verifies here.
func TestDocumentConcurrentScanAndAppend(t *testing.T) {
	files := make([]*diff.File, 20)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.go", i), 12, "changed")
	}

	loads := 0
	m := New(false, styles.New())
	m.SetSize(80, 10)
	m.SetDocument(func(i int) *diff.File {
		loads++
		return files[i]
	})
	m.AppendBlocks(files[:5])

	_, cmd := m.Search("changed")
	if cmd == nil {
		t.Fatal("the background scan should be scheduled")
	}

	done := make(chan interface{}, 1)
	go func() { done <- cmd() }()
	for i := 5; i < len(files); i++ {
		m.AppendBlocks(files[i : i+1])
	}

	raw := <-done
	msg, ok := raw.(SearchResultsMsg)
	if !ok {
		t.Fatalf("background scan returned %T", raw)
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("results of the running search should still apply")
	}
	if m.MatchCount() == 0 {
		t.Fatal("the scan found nothing")
	}
}

// docTriggerFile is the shape that used to crash the pager: a three-byte line
// immediately followed by a long one, both containing the search term. When
// the match coordinates were one row off, the long line's span was applied to
// the short one and the highlight slice ran past its end.
func docTriggerFile(path string) *diff.File {
	oldText := "a\nb\nc\n"
	newText := "key\n" + strings.Repeat("pad ", 10) + "key\n" + "key\n"
	return diff.Generate(path, diff.Modified, oldText, newText, diff.GenerateOptions{Context: 3})
}

func TestDocumentSearchMatchesItsOwnRow(t *testing.T) {
	files := []*diff.File{docTriggerFile("a.go"), docTriggerFile("b.go")}
	loads := 0
	m := newDocView(t, files, &loads)

	_, cmd := m.Search("key")
	if cmd == nil {
		t.Fatal("the background scan should be scheduled")
	}
	msg, ok := cmd().(SearchResultsMsg)
	if !ok {
		t.Fatalf("background scan returned %T", cmd())
	}
	if len(msg.Matches) < 2 {
		t.Fatalf("matches = %d, want several", len(msg.Matches))
	}
	if !m.ApplySearchResults(msg) {
		t.Fatal("fresh results should apply")
	}

	// This is where the panic surfaced: painting the window resolved the
	// neighbouring line's spans against this line's text.
	_ = m.View()

	for _, mt := range msg.Matches {
		row := m.rowAt(m.rowForLine(mt.Line))
		if !strings.Contains(strings.ToLower(row.line.Text), "key") {
			t.Fatalf("match at line %d points at %q", mt.Line, row.line.Text)
		}
		if mt.End > len(row.line.Text) {
			t.Fatalf("span %d:%d runs past %q", mt.Start, mt.End, row.line.Text)
		}
	}

	// The current match must highlight its own row, not the next one.
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) {
		t.Fatalf("match index = %d", m.matchIdx)
	}
	sel := m.matches[m.matchIdx]
	row := m.rowAt(m.rowForLine(sel.Line))
	if got := row.flat; got != sel.Line {
		t.Fatalf("selected match line %d does not match its row's flat index %d", sel.Line, got)
	}
}

func TestBoundarySetClampsStaleSpans(t *testing.T) {
	// Spans from elsewhere in the document must not survive as slice points.
	for _, spans := range [][]Match{
		{{Line: 0, Start: 10, End: 30}},
		{{Line: 0, Start: -5, End: -1}},
		{{Line: 0, Start: -100, End: 100}},
	} {
		for _, v := range boundarySet(0, 3, nil, spans) {
			if v < 0 || v > 3 {
				t.Fatalf("boundary %d outside [0,3] for spans %v", v, spans)
			}
		}
	}
}
