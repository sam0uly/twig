package stage

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
)

// gen diffs pre against post the way the staging path does: no whitespace
// tolerance, so a whitespace-only change is a change and not context.
func gen(t *testing.T, pre, post string) *diff.File {
	t.Helper()
	kind := diff.Modified
	switch {
	case pre == "":
		kind = diff.Added
	case post == "":
		kind = diff.Deleted
	}
	f := diff.Generate("f.txt", kind, pre, post, diff.GenerateOptions{Context: 3})
	if f == nil {
		t.Fatal("nil diff")
	}
	return f
}

// TestBlendExtremes pins the two properties the whole feature rests on: the
// selection is a subset of the change, so selecting everything must land
// exactly on the postimage and selecting nothing must leave the preimage
// untouched, byte for byte.
func TestBlendExtremes(t *testing.T) {
	cases := []struct {
		name, pre, post string
		err             error
	}{
		{"replace one line", "a\nb\nc\n", "a\nB\nc\n", nil},
		{"append", "a\nb\n", "a\nb\nc\n", nil},
		{"prepend", "b\nc\n", "a\nb\nc\n", nil},
		{"delete middle", "a\nb\nc\n", "a\nc\n", nil},
		{"insert several", "a\n", "a\nx\ny\nz\n", nil},
		{"adjacent changes", "a\nb\nc\n", "A\nB\nc\n", nil},
		{"two distant hunks", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "1\nX\n3\n4\n5\n6\n7\n8\nY\n", nil},
		{"no final newline in post", "a\nb\nc\n", "a\nb\nC", nil},
		{"no final newline either side", "a\nb", "a\nB", nil},
		{"empty to content", "", "one\ntwo\n", nil},
		{"content to empty", "one\ntwo\n", "", nil},
		{"crlf", "a\r\nb\r\n", "a\r\nB\r\n", nil},
		{"unicode", "héllo\nwörld\n", "héllo\nwörld!\n", nil},
		{"blank lines", "a\n\n\nb\n", "a\n\nb\n", nil},

		// The one shape a line selection cannot express: the last line is
		// carried over while its terminator is not, so a partial stage would
		// have to guess. These files are staged whole instead.
		{"no final newline in pre", "a\nb\nc", "a\nB\nc\n", ErrTailOnly},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := gen(t, c.pre, c.post)

			got, err := Blend(c.pre, c.post, f, AllSelection(f))
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("want %v, got %q, %v", c.err, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("all: %v", err)
			}
			if got != c.post {
				t.Fatalf("selecting everything must produce the postimage\ngot  %q\nwant %q", got, c.post)
			}

			if got, err := Blend(c.pre, c.post, f, NewSelection()); err != nil {
				t.Fatalf("none: %v", err)
			} else if got != c.pre {
				t.Fatalf("selecting nothing must produce the preimage\ngot  %q\nwant %q", got, c.pre)
			}
		})
	}
}

// TestBlendContextIndependence makes sure the context the viewer happens to be
// showing cannot change what a stage writes. A hunk window is a presentation
// choice; the blend only reads which lines changed, and one context line is
// enough to anchor an insertion in the preimage.
func TestBlendContextIndependence(t *testing.T) {
	pre := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"
	post := "1\nTWO\n3\n4\n5\n6\n7\n8\n9\n10\nELEVEN\n12\n"

	var want string
	for _, ctx := range []int{1, 2, 3, 8} {
		f := diff.Generate("f.txt", diff.Modified, pre, post, diff.GenerateOptions{Context: ctx})
		sel := AllSelection(f)
		got, err := Blend(pre, post, f, sel)
		if err != nil {
			t.Fatalf("context %d: %v", ctx, err)
		}
		if want == "" {
			want = got
		}
		if got != want {
			t.Fatalf("context %d changed the result\ngot  %q\nwant %q", ctx, got, want)
		}
		if got != post {
			t.Fatalf("context %d did not reproduce the postimage", ctx)
		}
	}
}

// TestBlendTailNewlineOnly covers a file whose only change is the final
// newline. There is no change line to select, so the diff view shows nothing
// and the file is staged whole; the blend must agree rather than invent a
// row.
func TestBlendTailNewlineOnly(t *testing.T) {
	for _, c := range [][2]string{
		{"a\nb", "a\nb\n"},
		{"a\nb\n", "a\nb"},
	} {
		f := gen(t, c[0], c[1])
		if sel := AllSelection(f); !sel.Empty() {
			t.Fatalf("%q -> %q: %d change lines, want none", c[0], c[1], sel.Len())
		}
		got, err := Blend(c[0], c[1], f, NewSelection())
		if err != nil {
			t.Fatalf("%q -> %q: %v", c[0], c[1], err)
		}
		if got != c[0] {
			t.Fatalf("got %q, want %q", got, c[0])
		}
	}
}

// TestBlendSingleLine stages one line out of a two-line change and checks the
// half-applied file is the one git would have written.
func TestBlendSingleLine(t *testing.T) {
	pre := "keep\nold one\nold two\nkeep\n"
	post := "keep\nnew one\nnew two\nkeep\n"
	f := gen(t, pre, post)

	// Only the second replacement.
	sel := NewSelection()
	sel.Add(NewPre(3))
	sel.Add(NewPost(3))

	got, err := Blend(pre, post, f, sel)
	if err != nil {
		t.Fatal(err)
	}
	want := "keep\nold one\nnew two\nkeep\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBlendHunkSelection stages the first hunk of a two-hunk file and leaves
// the second alone.
func TestBlendHunkSelection(t *testing.T) {
	pre := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	post := "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\nk\nl\n"
	f := gen(t, pre, post)
	if f.NHunks() != 2 {
		t.Fatalf("%d hunks, want 2", f.NHunks())
	}

	got, err := Blend(pre, post, f, HunkSelection(f, 0))
	if err != nil {
		t.Fatal(err)
	}
	want := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBlendInsertOnlySelection stages an added line without its neighbour.
func TestBlendInsertOnlySelection(t *testing.T) {
	pre := "a\nb\n"
	post := "a\nX\nY\nb\n"
	f := gen(t, pre, post)

	sel := NewSelection()
	sel.Add(NewPost(3)) // Y

	got, err := Blend(pre, post, f, sel)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\nY\nb\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBlendDeleteOnlySelection drops one of two deletions.
func TestBlendDeleteOnlySelection(t *testing.T) {
	pre := "a\nX\nY\nb\n"
	post := "a\nb\n"
	f := gen(t, pre, post)

	sel := NewSelection()
	sel.Add(NewPre(3)) // Y only

	got, err := Blend(pre, post, f, sel)
	if err != nil {
		t.Fatal(err)
	}
	if want := "a\nX\nb\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBlendRandomExtremes runs the two invariants over generated content,
// because hand-written cases miss the arrangements a real file produces.
func TestBlendRandomExtremes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	words := []string{"a", "b", "c", "", "x", "", "a b", "\t"}
	line := func() string {
		n := 1 + rng.Intn(3)
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, words[rng.Intn(len(words))])
		}
		return strings.Join(parts, " ")
	}

	for i := 0; i < 200; i++ {
		pre := randomFile(rng, line)
		post := randomFile(rng, line)
		f := gen(t, pre, post)

		got, err := Blend(pre, post, f, AllSelection(f))
		if errors.Is(err, ErrTailOnly) {
			// Only the final newline of an otherwise identical last line
			// moved; the caller stages such a file whole.
			continue
		}
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if got != post {
			t.Fatalf("case %d: all-selected blend diverged\npre  %q\npost %q\ngot  %q", i, pre, post, got)
		}

		got, err = Blend(pre, post, f, NewSelection())
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if got != pre {
			t.Fatalf("case %d: empty blend diverged\ngot  %q\nwant %q", i, got, pre)
		}
	}
}

func randomFile(rng *rand.Rand, line func() string) string {
	n := rng.Intn(12)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(line())
		b.WriteByte('\n')
	}
	if n > 0 && rng.Intn(4) == 0 {
		// Drop the final newline: a blend must not invent one.
		s := b.String()
		return strings.TrimSuffix(s, "\n")
	}
	return b.String()
}

// TestSelectionToggle covers the state a keystroke drives.
func TestSelectionToggle(t *testing.T) {
	s := NewSelection()
	l := NewPost(4)
	if s.Toggle(l) != true || !s.Has(l) || s.Len() != 1 {
		t.Fatal("toggle on should select")
	}
	if s.Toggle(l) != false || s.Has(l) || s.Len() != 0 {
		t.Fatal("toggle off should deselect")
	}

	other := NewSelection()
	other.Add(NewPre(1))
	s.Merge(other)
	if !s.Equal(other) {
		t.Fatal("merge should copy the lines")
	}
	c := s.Clone()
	c.Remove(NewPre(1))
	if s.Empty() {
		t.Fatal("editing a clone must not touch the original")
	}
}
