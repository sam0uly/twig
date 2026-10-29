package diff

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

func kindsOf(f *File) []LineKind {
	var kinds []LineKind
	for _, l := range f.Lines {
		kinds = append(kinds, l.Kind)
	}
	return kinds
}

// TestGenerateStress throws deterministic random diffs of many shapes at
// Generate — including shapes beyond the LCS bound and changes clustered at
// the tail — and checks the invariants hold.
func TestGenerateStress(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(42, 7))
	for round := 0; round < 300; round++ {
		n := rng.IntN(400)
		var oldB, newB strings.Builder
		for i := 0; i < n; i++ {
			line := fmt.Sprintf("line %d payload %d", i, rng.IntN(50))
			oldB.WriteString(line)
			oldB.WriteByte('\n')
			switch rng.IntN(6) {
			case 0: // modify
				newB.WriteString(strings.ToUpper(line))
			case 1: // delete (skip)
			case 2: // insert extra
				newB.WriteString(line)
				newB.WriteString("inserted extra\n")
			default:
				newB.WriteString(line)
			}
		}

		f := Generate("stress.txt", Modified, oldB.String(), newB.String(),
			GenerateOptions{Context: rng.IntN(6)})

		var adds, dels, lastOld, lastNew int
		for _, h := range f.Hunks {
			if h.LineIndex < 0 || h.LineIndex >= len(f.Lines) {
				t.Fatalf("round %d: hunk LineIndex %d out of range (%d lines)",
					round, h.LineIndex, len(f.Lines))
			}
			if f.Lines[h.LineIndex].Kind != HunkHeader {
				t.Fatalf("round %d: LineIndex %d not a hunk header", round, h.LineIndex)
			}
		}
		for _, l := range f.Lines {
			switch l.Kind {
			case Addition:
				adds++
				if l.New <= lastNew && lastNew > 0 {
					t.Fatalf("round %d: new numbers not increasing at %q", round, l.Text)
				}
				if l.New > 0 {
					lastNew = l.New
				}
			case Deletion:
				dels++
				if l.Old <= lastOld && lastOld > 0 {
					t.Fatalf("round %d: old numbers not increasing at %q", round, l.Text)
				}
				if l.Old > 0 {
					lastOld = l.Old
				}
			}
		}
		if f.Additions != adds || f.Deletions != dels {
			t.Fatalf("round %d: stats %d/%d != counted %d/%d",
				round, f.Additions, f.Deletions, adds, dels)
		}
	}
}

func TestGenerateModified(t *testing.T) {
	t.Parallel()

	f := Generate("a.txt", Modified,
		"one\ntwo\nthree\n",
		"one\nTWO\nthree\nfour\n",
		GenerateOptions{Context: 3})

	if f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("stats = %d/%d, want 2/1", f.Additions, f.Deletions)
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.Header != "@@ -1,3 +1,4 @@" {
		t.Errorf("header = %q", h.Header)
	}
	want := []LineKind{
		HunkHeader, Context, Deletion, Addition, Context, Addition,
	}
	got := kindsOf(f)
	if strings.Join(mapKinds(got), ",") != strings.Join(mapKinds(want), ",") {
		t.Errorf("kinds = %v, want %v", mapKinds(got), mapKinds(want))
	}

	// Line numbers.
	if f.Lines[1].Old != 1 || f.Lines[1].New != 1 {
		t.Errorf("context numbers = %d/%d, want 1/1", f.Lines[1].Old, f.Lines[1].New)
	}
	if f.Lines[2].Old != 2 || f.Lines[2].New != 0 {
		t.Errorf("del numbers = %d/%d, want 2/0", f.Lines[2].Old, f.Lines[2].New)
	}
	if f.Lines[3].New != 2 || f.Lines[3].Old != 0 {
		t.Errorf("add numbers = %d/%d, want 0/2", f.Lines[3].Old, f.Lines[3].New)
	}
}

func TestGenerateNewAndDeleted(t *testing.T) {
	t.Parallel()

	f := Generate("new.md", Added, "", "a\nb\n", GenerateOptions{Context: 3})
	if f.Additions != 2 || f.Deletions != 0 {
		t.Errorf("add stats = %d/%d", f.Additions, f.Deletions)
	}
	if f.Hunks[0].Header != "@@ -0,0 +1,2 @@" {
		t.Errorf("header = %q", f.Hunks[0].Header)
	}

	f = Generate("old.go", Deleted, "x\ny\n", "", GenerateOptions{Context: 3})
	if f.Deletions != 2 || f.Additions != 0 {
		t.Errorf("del stats = %d/%d", f.Additions, f.Deletions)
	}
	if f.Hunks[0].Header != "@@ -1,2 +0,0 @@" {
		t.Errorf("header = %q", f.Hunks[0].Header)
	}
}

func TestGenerateSeparateHunks(t *testing.T) {
	t.Parallel()

	oldText := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n"
	newText := "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\nP\n"
	f := Generate("x.txt", Modified, oldText, newText, GenerateOptions{Context: 2})

	if len(f.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2: %v", len(f.Hunks), kindsOf(f))
	}
	if f.Hunks[0].Header != "@@ -1,3 +1,3 @@" {
		t.Errorf("hunk1 = %q", f.Hunks[0].Header)
	}
	if f.Hunks[1].Header != "@@ -14,3 +14,3 @@" {
		t.Errorf("hunk2 = %q", f.Hunks[1].Header)
	}
}

func TestGenerateContextMerge(t *testing.T) {
	t.Parallel()

	// Two changes separated by exactly 2*context equal lines merge into
	// one hunk because the context regions touch.
	oldText := "a\nb\nc\nd\ne\n"
	newText := "A\nb\nc\nd\nE\n"
	f := Generate("x.txt", Modified, oldText, newText, GenerateOptions{Context: 2})
	if len(f.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1 (merged)", len(f.Hunks))
	}
	if f.Hunks[0].Header != "@@ -1,5 +1,5 @@" {
		t.Errorf("header = %q", f.Hunks[0].Header)
	}
}

func TestGenerateContextNotMerged(t *testing.T) {
	t.Parallel()

	// Changes separated by more than 2*context equal lines stay separate.
	oldText := "a\nb\nc\nd\ne\nf\ng\nh\ni\n"
	newText := "A\nb\nc\nd\ne\nf\ng\nh\nI\n"
	f := Generate("x.txt", Modified, oldText, newText, GenerateOptions{Context: 2})
	if len(f.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(f.Hunks))
	}
	if f.Hunks[0].Header != "@@ -1,3 +1,3 @@" {
		t.Errorf("hunk1 = %q", f.Hunks[0].Header)
	}
	if f.Hunks[1].Header != "@@ -7,3 +7,3 @@" {
		t.Errorf("hunk2 = %q", f.Hunks[1].Header)
	}
}

func TestGenerateIgnoreWhitespace(t *testing.T) {
	t.Parallel()

	f := Generate("x.txt", Modified,
		"one  two\nthree\n",
		"one two\nthree\n",
		GenerateOptions{Context: 3, IgnoreWhitespace: true})

	if f.Additions != 0 || f.Deletions != 0 {
		t.Errorf("stats = %d/%d, want 0/0 with ignore-whitespace", f.Additions, f.Deletions)
	}
	if len(f.Lines) != 0 {
		t.Errorf("lines = %d, want 0", len(f.Lines))
	}
}

func TestGenerateNoTrailingNewline(t *testing.T) {
	t.Parallel()

	f := Generate("x.txt", Modified, "a\nb", "a\nb\nc", GenerateOptions{Context: 3})
	if f.Additions != 1 {
		t.Errorf("additions = %d, want 1", f.Additions)
	}
	last := f.Lines[len(f.Lines)-1]
	if last.Text != "+c" {
		t.Errorf("last line = %q", last.Text)
	}
}

func TestGenerateBinary(t *testing.T) {
	t.Parallel()

	f := Generate("img.png", Modified, "a\x00b", "c\x00d", GenerateOptions{Context: 3})
	if !f.Binary {
		t.Error("expected binary")
	}
	if len(f.Lines) != 1 || f.Lines[0].Text != "Binary file differs" {
		t.Errorf("lines = %v", f.Lines)
	}
}

func TestGenerateWordDiffIntegrated(t *testing.T) {
	t.Parallel()

	f := Generate("greet.js", Modified,
		"function greet() {\n  const greeting = 'hello';\n}\n",
		"function greet() {\n  const greeting = 'hi';\n}\n",
		GenerateOptions{Context: 3})

	var del, add Line
	for _, l := range f.Lines {
		switch l.Kind {
		case Deletion:
			del = l
		case Addition:
			add = l
		}
	}
	if del.Text == "" || add.Text == "" {
		t.Fatal("missing changed lines")
	}
	if !spansEqual(del.WordRanges(), []Span{{21, 26}}) {
		t.Errorf("del ranges = %v, want [{21 26}]", del.WordRanges())
	}
	if !spansEqual(add.WordRanges(), []Span{{21, 23}}) {
		t.Errorf("add ranges = %v, want [{21 23}]", add.WordRanges())
	}
}

func TestGenerateIdentical(t *testing.T) {
	t.Parallel()

	f := Generate("x.txt", Modified, "a\nb\n", "a\nb\n", GenerateOptions{Context: 3})
	if len(f.Lines) != 0 || len(f.Hunks) != 0 {
		t.Errorf("identical contents produced %d lines", len(f.Lines))
	}
}

func mapKinds(kinds []LineKind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		switch k {
		case Context:
			out[i] = "ctx"
		case Addition:
			out[i] = "add"
		case Deletion:
			out[i] = "del"
		case HunkHeader:
			out[i] = "hunk"
		default:
			out[i] = "?"
		}
	}
	return out
}
