package diff

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
)

// resident builds the same diff without paging, which is the reference the
// paged representation has to reproduce exactly.
func resident(t *testing.T, oldText, newText string, opts GenerateOptions) *File {
	t.Helper()
	f := generate("f", Modified, oldText, newText, opts, math.MaxInt)
	if f.Paged() {
		t.Fatal("resident reference is paged")
	}
	return f
}

func paged(t *testing.T, oldText, newText string, opts GenerateOptions) *File {
	t.Helper()
	f := generate("f", Modified, oldText, newText, opts, 0)
	if !f.Paged() && f.NLines() > 0 {
		t.Fatal("paged reference is resident")
	}
	return f
}

// TestTextMatchesSplitLines pins that the offset index produces exactly the
// lines the old splitter produced: the whole engine reads lines through it.
func TestTextMatchesSplitLines(t *testing.T) {
	cases := []string{
		"", "a", "a\n", "a\nb", "a\nb\n", "\n", "\n\n", "a\n\n", "a\r\nb\r\n",
		"\r\n", "a\n\n\n", "  \n\t\n", "a\rb\nc", "\r", "a\nb\r",
	}
	for _, s := range cases {
		want := splitLines(s)
		got := indexText(s)
		if got.n() != len(want) {
			t.Fatalf("%q: %d lines, want %d", s, got.n(), len(want))
		}
		for i := range want {
			if got.line(i) != want[i] {
				t.Fatalf("%q line %d = %q, want %q", s, i, got.line(i), want[i])
			}
			if got.lineLen(i) != len(want[i]) {
				t.Fatalf("%q line %d len = %d, want %d", s, i, got.lineLen(i), len(want[i]))
			}
		}
	}
}

// TestGeneratePagesLargeDiff checks the switch itself: an ordinary file stays
// resident, a multi-million row one keeps only descriptors.
func TestGeneratePagesLargeDiff(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 20_000; i++ {
		a.WriteString("line a\n")
		b.WriteString("line b\n")
	}

	big := generate("f", Modified, a.String(), b.String(), GenerateOptions{Context: 3}, pageRows)
	if !big.Paged() {
		t.Fatal("a 40k row diff should be paged")
	}
	if len(big.Lines) != 0 {
		t.Fatalf("paged file keeps %d lines, want 0", len(big.Lines))
	}
	if big.NLines() != 40_001 {
		t.Fatalf("rows = %d, want 40001", big.NLines())
	}

	small := Generate("f", Modified, "a\nb\n", "a\nc\n", GenerateOptions{Context: 3})
	if small.Paged() || len(small.Lines) == 0 {
		t.Fatal("a tiny diff should stay resident")
	}

	for _, f := range []*File{big, small} {
		if f.Additions != 20_000 && f.Path() == "f" && f == big {
			t.Fatalf("additions = %d, want 20000", f.Additions)
		}
	}
}

// TestPagedMatchesResident is the parity check: every row, line number, hunk,
// word span and the rendered patch must be identical in both layouts.
func TestPagedMatchesResident(t *testing.T) {
	alphabet := []string{"a", "b", "", "x", "  y", "if err != nil {", "z", "a\tb"}
	r := rand.New(rand.NewSource(11))

	gen := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
			b.WriteString("\n")
		}
		return b.String()
	}

	for it := 0; it < 60; it++ {
		opts := GenerateOptions{Context: r.Intn(5)}
		if it%3 == 0 {
			opts.IgnoreWhitespace = true
		}
		oldText, newText := gen(r.Intn(400)), gen(r.Intn(400))

		want := resident(t, oldText, newText, opts)
		got := paged(t, oldText, newText, opts)

		if got.NLines() != want.NLines() {
			t.Fatalf("it=%d: rows = %d, want %d", it, got.NLines(), want.NLines())
		}
		if got.Additions != want.Additions || got.Deletions != want.Deletions {
			t.Fatalf("it=%d: +%d -%d, want +%d -%d", it,
				got.Additions, got.Deletions, want.Additions, want.Deletions)
		}
		if got.NHunks() != want.NHunks() {
			t.Fatalf("it=%d: hunks = %d, want %d", it, got.NHunks(), want.NHunks())
		}

		for i := 0; i < want.NLines(); i++ {
			wl, gl := want.Lines[i], mustLine(t, got, i)
			if gl.Kind != wl.Kind || gl.Text != wl.Text || gl.Old != wl.Old || gl.New != wl.New {
				t.Fatalf("it=%d row %d: got %+v, want %+v", it, i, gl, wl)
			}
			if got.LineText(i) != wl.Text {
				t.Fatalf("it=%d row %d text = %q, want %q", it, i, got.LineText(i), wl.Text)
			}
		}

		for i := 0; i < want.NHunks(); i++ {
			wh, _ := want.HunkAt(i)
			gh, ok := got.HunkAt(i)
			if !ok {
				t.Fatalf("it=%d: hunk %d missing", it, i)
			}
			if gh.Header != wh.Header || gh.LineIndex != wh.LineIndex {
				t.Fatalf("it=%d hunk %d: got %q@%d, want %q@%d", it, i,
					gh.Header, gh.LineIndex, wh.Header, wh.LineIndex)
			}
			if len(gh.Lines) != len(wh.Lines) {
				t.Fatalf("it=%d hunk %d: %d lines, want %d", it, i, len(gh.Lines), len(wh.Lines))
			}
			for j := range wh.Lines {
				gl, wl := gh.Lines[j], wh.Lines[j]
				if gl.Text != wl.Text || gl.Old != wl.Old || gl.New != wl.New {
					t.Fatalf("it=%d hunk %d line %d: got %+v, want %+v", it, i, j, gl, wl)
				}
				if spansDiffer(gl.words, wl.words) {
					t.Fatalf("it=%d hunk %d line %d: spans %v, want %v", it, i, j, gl.words, wl.words)
				}
			}
		}

		if gp, wp := got.Patch(), want.Patch(); gp != wp {
			t.Fatalf("it=%d: patch differs\n got:\n%s\nwant:\n%s", it, gp, wp)
		}
	}
}

func mustLine(t *testing.T, f *File, i int) Line {
	t.Helper()
	l, ok := f.LineAt(i)
	if !ok {
		t.Fatalf("line %d out of range (%d lines)", i, f.NLines())
	}
	return l
}

func spansDiffer(a, b []Span) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// TestPagedChunksMatch pins that arbitrary page boundaries rebuild the same
// rows: the checkpoints and the per-hunk cursors must survive a page that
// starts in the middle of a hunk.
func TestPagedChunksMatch(t *testing.T) {
	alphabet := []string{"a", "b", "", "x", "  y", "if err != nil {"}
	r := rand.New(rand.NewSource(3))

	var a, b strings.Builder
	for i := 0; i < 4000; i++ {
		a.WriteString(alphabet[r.Intn(len(alphabet))])
		a.WriteString("\n")
		if i%3 == 0 {
			continue
		}
		b.WriteString(alphabet[r.Intn(len(alphabet))])
		b.WriteString("\n")
	}

	want := resident(t, a.String(), b.String(), GenerateOptions{Context: 2})
	got := paged(t, a.String(), b.String(), GenerateOptions{Context: 2})
	if got.NLines() != want.NLines() {
		t.Fatalf("rows = %d, want %d", got.NLines(), want.NLines())
	}

	for _, size := range []int{1, 7, 64, 65, 2048} {
		for from := 0; from < got.NLines(); from += size {
			to := min(from+size, got.NLines())
			chunk := got.LinesRange(from, to)
			if len(chunk) != to-from {
				t.Fatalf("size %d: range [%d,%d) returned %d lines", size, from, to, len(chunk))
			}
			for i, l := range chunk {
				w := want.Lines[from+i]
				if l.Kind != w.Kind || l.Text != w.Text || l.Old != w.Old || l.New != w.New {
					t.Fatalf("size %d row %d: got %+v, want %+v", size, from+i, l, w)
				}
			}
		}
	}
}

// TestPagedScanMatchesLines pins the layout stream against the rows it
// reports, so a paged document lays out to the same height as a flat one.
func TestPagedScanMatchesLines(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 5000; i++ {
		a.WriteString("same line\n")
		if i%97 == 0 {
			b.WriteString("changed line\n")
			continue
		}
		b.WriteString("same line\n")
	}

	got := paged(t, a.String(), b.String(), GenerateOptions{Context: 3})
	want := resident(t, a.String(), b.String(), GenerateOptions{Context: 3})

	var kinds []LineKind
	var widths []int
	got.ScanLines(0, got.NLines(), func(kind LineKind, width int) {
		kinds = append(kinds, kind)
		widths = append(widths, width)
	})
	if len(kinds) != want.NLines() {
		t.Fatalf("scanned %d rows, want %d", len(kinds), want.NLines())
	}
	for i := range kinds {
		if kinds[i] != want.Lines[i].Kind {
			t.Fatalf("row %d kind = %d, want %d", i, kinds[i], want.Lines[i].Kind)
		}
		if widths[i] != len(want.Lines[i].Text) {
			t.Fatalf("row %d width = %d, want %d", i, widths[i], len(want.Lines[i].Text))
		}
	}
}

// TestPagedHunkBodyIsBounded checks that a paged hunk hands back only its own
// body, the same guarantee the resident layout gives.
func TestPagedHunkBodyIsBounded(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 3000; i++ {
		if i%500 == 0 {
			a.WriteString("old\n")
			b.WriteString("new\n")
			continue
		}
		a.WriteString("same\n")
		b.WriteString("same\n")
	}

	f := paged(t, a.String(), b.String(), GenerateOptions{Context: 2})
	if f.NHunks() != 6 {
		t.Fatalf("hunks = %d, want 6", f.NHunks())
	}
	for i := range f.NHunks() {
		h, ok := f.HunkAt(i)
		if !ok {
			t.Fatalf("hunk %d missing", i)
		}
		// The first change sits on line 1, so it has no leading context;
		// every later one shows two lines either side of the replacement.
		body := 6
		if i == 0 {
			body = 4
		}
		if len(h.Lines) != body {
			t.Fatalf("hunk %d body = %d lines, want %d", i, len(h.Lines), body)
		}
		if n := strings.Count(f.HunkPatch(i), "\n"); n != body+1 {
			t.Fatalf("hunk %d patch has %d lines, want %d", i, n, body+1)
		}
	}
}

// BenchmarkGenerateLargePaged measures the same diff with paging forced. It is
// the shape a big file takes in the viewer: rows are descriptors and no line is
// rendered until a page is painted, so it should stay far below the resident
// benchmark while allocating about as much as the diff itself.
func BenchmarkGenerateLargePaged(b *testing.B) {
	var oldB strings.Builder
	for i := 0; i < 5000; i++ {
		oldB.WriteString("func line")
		oldB.WriteString(strings.Repeat("x", i%5))
		oldB.WriteString("() { return nil }\n")
	}
	oldText := oldB.String()
	newText := strings.Replace(oldText, "return nil", "return err", 400)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generate("f", Modified, oldText, newText, GenerateOptions{Context: 3}, 0)
	}
}

// TestWholeFilePlanIsConstant pins the shape a whole-file addition takes: one
// hunk, one run. Interning a million lines to discover nothing matches is the
// expensive half of opening a large new file, so it must not happen at all.
func TestWholeFilePlanIsConstant(t *testing.T) {
	var newText strings.Builder
	for i := 0; i < 200_000; i++ {
		fmt.Fprintf(&newText, "line %d payload\n", i)
	}
	oldText := ""

	f := paged(t, oldText, newText.String(), GenerateOptions{Context: 3})
	if f.NLines() != 200_001 {
		t.Fatalf("rows = %d, want 200001", f.NLines())
	}
	if len(f.page.ops) != 2 {
		t.Fatalf("plan has %d runs, want 2", len(f.page.ops))
	}
	if f.Additions != 200_000 || f.Deletions != 0 {
		t.Fatalf("+%d -%d, want +200000 -0", f.Additions, f.Deletions)
	}
	if f.NHunks() != 1 {
		t.Fatalf("hunks = %d, want 1", f.NHunks())
	}
	// The plan is a couple of runs plus the sources, so its estimate must be
	// close to the content itself rather than a multiple of it.
	if got, content := f.Bytes(), len(newText.String()); got > content+content/4 {
		t.Fatalf("bytes = %d for %d bytes of content", got, content)
	}

	var kinds []LineKind
	f.ScanLines(0, 3, func(kind LineKind, _ int) { kinds = append(kinds, kind) })
	if len(kinds) != 3 || kinds[0] != HunkHeader || kinds[1] != Addition || kinds[2] != Addition {
		t.Fatalf("kinds = %v", kinds)
	}
}

// BenchmarkGenerateWholeFileAdd measures the case that used to dominate: a
// file that exists on one side only.
func BenchmarkGenerateWholeFileAdd(b *testing.B) {
	var newText strings.Builder
	for i := 0; i < 100_000; i++ {
		fmt.Fprintf(&newText, "line %d payload payload\n", i)
	}
	text := newText.String()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generate("f", Added, "", text, GenerateOptions{Context: 3}, 0)
	}
}

// TestAnchoredLargeDiff checks the anchoring pass on a file too large for an
// exact script: ten scattered edits must produce exactly ten hunks, which a
// false anchor would break.
func TestAnchoredLargeDiff(t *testing.T) {
	var oldB, newB strings.Builder
	const lines = 20_000
	for i := 0; i < lines; i++ {
		if i%2000 == 1000 {
			fmt.Fprintf(&oldB, "line %d before\n", i)
			fmt.Fprintf(&newB, "line %d after\n", i)
			continue
		}
		fmt.Fprintf(&oldB, "line %d payload payload payload\n", i)
		fmt.Fprintf(&newB, "line %d payload payload payload\n", i)
	}
	oldText, newText := oldB.String(), newB.String()

	if n := indexText(oldText).n(); n <= exactRows {
		t.Fatalf("fixture has %d lines, needs to exceed the exact limit", n)
	}

	for _, f := range []*File{
		generate("f", Modified, oldText, newText, GenerateOptions{Context: 3}, 0),
		Generate("f", Modified, oldText, newText, GenerateOptions{Context: 3}),
	} {
		if f.Additions != lines/2000 || f.Deletions != lines/2000 {
			t.Fatalf("+%d -%d, want +%d -%d", f.Additions, f.Deletions, lines/2000, lines/2000)
		}
		if f.NHunks() != lines/2000 {
			t.Fatalf("hunks = %d, want %d", f.NHunks(), lines/2000)
		}
		for i := range f.NHunks() {
			h, _ := f.HunkAt(i)
			if len(h.Lines) != 8 {
				t.Fatalf("hunk %d body = %d lines, want 8", i, len(h.Lines))
			}
		}
	}
}
