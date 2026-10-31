package syntax

import "testing"

// spansOf renders an index line as classes and text for comparison.
func spansOf(run []uint32, src string) []tok {
	out := make([]tok, 0, len(run))
	for _, w := range run {
		start, end, class := UnpackToken(w)
		out = append(out, tok{class, src[start:end]})
	}
	return out
}

func TestUnpackToken(t *testing.T) {
	cases := []Span{
		{0, 1, Keyword},
		{3, 9, String},
		{0, 4095, Comment},
		{4094, 4095, Punct},
	}
	for _, s := range cases {
		w, ok := packSpan(s)
		if !ok {
			t.Fatalf("%v did not pack", s)
		}
		start, end, class := UnpackToken(w)
		if start != s.Start || end != s.End || class != s.Class {
			t.Fatalf("unpacked %v, want %v", Span{start, end, class}, s)
		}
	}
	if _, ok := packSpan(Span{0, 5000, String}); ok {
		t.Fatal("a span past the offset limit packed")
	}
	if _, ok := packSpan(Span{4, 4, String}); ok {
		t.Fatal("an empty span packed")
	}
}

// TestBuilderSides checks that each side keeps its own scanner state, which is
// what colours a deletion that opens a comment and a context line that closes
// it.
func TestBuilderSides(t *testing.T) {
	b := NewBuilder(goLang)
	b.Old("/* start", 1)
	b.New("x := 1", 1)
	b.Context("   middle */ y", 2, 2)
	idx := b.Index()

	if got := spansOf(idx.Old(1), "/* start"); len(got) != 1 || got[0] != (tok{Comment, "/* start"}) {
		t.Fatalf("old line 1 = %v", got)
	}
	newLine := "x := 1"
	if got := spansOf(idx.New(1), newLine); len(got) != 2 || got[0] != (tok{Operator, ":="}) || got[1] != (tok{Number, "1"}) {
		t.Fatalf("new line 1 = %v", got)
	}
	// The old side is still inside the comment; the new side never was, so its
	// stray "*/" is two operators rather than the end of anything.
	oldLine := "   middle */ y"
	if got := spansOf(idx.Old(2), oldLine); len(got) != 1 || got[0] != (tok{Comment, "   middle */"}) {
		t.Fatalf("old line 2 = %v", got)
	}
	if got := spansOf(idx.New(2), oldLine); len(got) != 1 || got[0] != (tok{Operator, "*/"}) {
		t.Fatalf("new line 2 = %v", got)
	}
}

// TestBuilderMatchesLines requires the index to agree with tokenising the same
// lines one at a time.
func TestBuilderMatchesLines(t *testing.T) {
	lines := loadFile(t, "testdata/sample.go")
	b := NewBuilder(goLang)
	var st State
	var buf []Span
	for i, line := range lines {
		buf, st = goLang.Line(buf[:0], line, st)
		b.Context(line, i+1, i+1)
		if want, got := buf, spansOf(b.Index().Old(i+1), line); !equalTokens(got, tokens(line, want)) {
			t.Fatalf("line %d: index %v, direct %v", i+1, got, tokens(line, want))
		}
	}
}

// TestIndexMissingLine covers a line the index never saw, which the renderer
// hits for rows that were never laid out.
func TestIndexMissingLine(t *testing.T) {
	b := NewBuilder(goLang)
	b.New("x := 1", 1)
	idx := b.Index()
	if got := idx.New(9); got != nil {
		t.Fatalf("line 9 = %v, want nil", got)
	}
	if got := idx.Old(1); got != nil {
		t.Fatalf("old line 1 = %v, want nil", got)
	}
	if n := idx.Bytes(); n <= 0 {
		t.Fatalf("Bytes() = %d", n)
	}
	if idx.Language() != goLang {
		t.Fatal("wrong language")
	}
	var nilIdx *Index
	if nilIdx.Old(1) != nil || nilIdx.New(1) != nil || nilIdx.Bytes() != 0 || nilIdx.Language() != nil {
		t.Fatal("a nil index must be harmless")
	}
	if NewBuilder(nil) != nil {
		t.Fatal("a nil language must yield a nil builder")
	}
}
