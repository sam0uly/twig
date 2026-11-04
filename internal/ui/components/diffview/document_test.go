package diffview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
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

// TestBlockBandsNameTheirSide: a file that is listed twice is in the document
// as two blocks whose bands are otherwise identical, so the band has to carry
// the side. A path that is listed once stays bare.
func TestBlockBandsNameTheirSide(t *testing.T) {
	files := []*diff.File{
		docTestFile("semi.txt", 4, "STAGED"),
		docTestFile("semi.txt", 4, "WORKTREE"),
		docTestFile("solo.txt", 4, "SOLO"),
	}
	m := New(false, styles.New())
	m.SetSize(80, 30)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, []BlockSide{
		{Staged: true, Tag: "  · staged"},
		{Tag: "  · unstaged"},
		{},
	})

	view := ansi.Strip(m.View())
	for _, want := range []string{"── semi.txt  · staged", "── semi.txt  · unstaged"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing band %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "── solo.txt  ·") {
		t.Fatalf("a single block named its side anyway:\n%s", view)
	}

	// The tag is decoration; the side a keystroke acts on still comes from the
	// block.
	m.SetSelectable(true)
	if !m.PlaceCursor() {
		t.Fatal("no cursor in the document")
	}
	if got, ok := m.CursorStaged(); !ok || !got {
		t.Fatalf("first block reads staged=%v ok=%v", got, ok)
	}
}

// TestCursorToBandLandsInTheBlock: the document keeps only a window of blocks
// materialised, so placing the cursor in a band has to be able to ask for that
// block's rows; a search over unmaterialised rows finds nothing and leaves the
// cursor somewhere else entirely.
func TestCursorToBandLandsInTheBlock(t *testing.T) {
	const n = 12
	files := make([]*diff.File, n)
	sides := make([]BlockSide, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.txt", i+1), 4, "CHANGED")
	}
	m := New(false, styles.New())
	m.SetSize(80, 24)
	m.SetSelectable(true)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, sides)

	const want = 7
	band := m.BandRow(want)
	if band <= 0 {
		t.Fatalf("band %d is at row %d", want, band)
	}
	m.JumpToBandNow(want)
	if got := m.YOffset(); got != band {
		t.Fatalf("the document is at %d, want the band at %d", got, band)
	}
	if !m.CursorToBand(want) {
		t.Fatalf("no row to place the cursor on in the band at %d", band)
	}
	if got := m.CursorRow(); got < band || got >= m.BandRow(want+1) {
		t.Fatalf("cursor on row %d, want inside the block at %d", got, band)
	}
}

// TestHunkNavigationTakesTheCursor: ] and [ used to scroll the window and leave
// the cursor behind, so the next keystroke acted on a row the reader could not
// see. The cursor goes to the hunk, and in a document that means across blocks.
func TestHunkNavigationTakesTheCursor(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.txt", 40, "FIRST"),
		docTestFile("b.txt", 40, "SECOND"),
	}
	m := New(false, styles.New())
	m.SetSize(80, 12)
	m.SetSmooth(false)
	m.SetSelectable(true)
	m.SetCursorFocus(true)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, make([]BlockSide, len(files)))

	m.GotoTop()
	if !m.HasCursor() {
		t.Fatal("no cursor at the top")
	}

	m.NextHunk()
	idx := m.CurrentHunkIndex()
	if idx < 0 || idx >= len(m.hunks) {
		t.Fatalf("hunk index %d for %d hunks", idx, len(m.hunks))
	}
	if got, want := m.CursorRow(), m.hunks[idx]; got != want {
		t.Fatalf("cursor on row %d, want the hunk header at %d", got, want)
	}
	first := m.hunks[idx]

	// The next hunk is in the other block: the cursor has to cross into it.
	m.NextHunk()
	idx = m.CurrentHunkIndex()
	if idx < 0 || idx >= len(m.hunks) {
		t.Fatalf("hunk index %d for %d hunks", idx, len(m.hunks))
	}
	second := m.hunks[idx]
	if second <= first {
		t.Fatalf("the second hunk at %d is not past the first at %d", second, first)
	}
	if got := m.CursorRow(); got != second {
		t.Fatalf("cursor on row %d, want the hunk header at %d", got, second)
	}
	if second < m.BandRow(1) {
		t.Fatalf("the second hunk at %d is still in the first block", second)
	}

	m.PrevHunk()
	if got := m.CursorRow(); got != first {
		t.Fatalf("prev hunk put the cursor on row %d, want %d", got, first)
	}
}

// TestGotoBottomLandsOnTheLastChange: the last block of a document is not
// materialised until something asks for it, and a row whose block is evicted
// answers nothing, so G used to stop on a hunk header in the middle of the
// document.
func TestGotoBottomLandsOnTheLastChange(t *testing.T) {
	const n = 12
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.txt", i+1), 40, "LAST")
	}
	m := New(false, styles.New())
	m.SetSize(80, 16)
	m.SetSelectable(true)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, make([]BlockSide, n))

	m.GotoBottom()
	if !m.HasCursor() {
		t.Fatal("no cursor after G")
	}
	row := m.CursorRow()
	if want := m.lastItemRow(); row != want {
		t.Fatalf("cursor on row %d, want the last row that acts at %d", row, want)
	}
	if row < m.BandRow(n-1) {
		t.Fatalf("cursor on row %d, want it in the last block at %d", row, m.BandRow(n-1))
	}
	if !m.cursorInWindow() {
		t.Fatalf("the last row at %d is off the window at %d", row, m.YOffset())
	}
}

// TestGotoBottomGlidesToTheEnd: the end of a document is far away, so G is the
// move most likely to be cut short by a glide that stops halfway.
func TestGotoBottomGlidesToTheEnd(t *testing.T) {
	const n = 12
	files := make([]*diff.File, n)
	for i := range files {
		files[i] = docTestFile(fmt.Sprintf("f%d.txt", i+1), 40, "LAST")
	}
	m := New(true, styles.New())
	m.SetSize(80, 16)
	m.SetSelectable(true)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, make([]BlockSide, n))
	m.ensureContent()

	m.GotoBottom()
	if !m.anim {
		t.Fatal("G did not start a glide")
	}
	at := time.Now()
	for i := 0; i < 400 && m.anim; i++ {
		at = at.Add(frameInterval)
		next, _ := m.Update(scrollFrameMsg{seq: m.animSeq, at: at})
		m = next
	}
	if m.anim {
		t.Fatal("the glide never settled")
	}
	if got, want := m.YOffset(), m.maxScroll(); got != want {
		t.Fatalf("the glide stopped at %d, want the end of the diff at %d", got, want)
	}
	if !m.cursorInWindow() {
		t.Fatalf("the cursor at %d is off the window at %d", m.CursorRow(), m.YOffset())
	}
}
