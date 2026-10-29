package diff

import (
	"fmt"
	"strings"
	"testing"
)

// scanRendered is the reference the source scan has to match: fold every
// rendered line and search it, exactly as the viewer does without the fast path.
func scanRendered(f *File, term string) []MatchRow {
	lower := strings.ToLower(term)
	var out []MatchRow
	for i := 0; i < f.NLines(); i++ {
		line := strings.ToLower(f.LineText(i))
		for at := 0; at <= len(line)-len(lower); {
			j := strings.Index(line[at:], lower)
			if j < 0 {
				break
			}
			start := at + j
			out = append(out, MatchRow{Row: i, Start: start, End: start + len(lower)})
			at = start + len(lower)
		}
	}
	return out
}

// TestPlanMatchesRenderedLines checks the source scan against the rendered line
// scan on a diff with context, deletions and additions: the matches must be
// identical, columns included.
func TestPlanMatchesRenderedLines(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 4000; i++ {
		switch i % 97 {
		case 0:
			fmt.Fprintf(&oldB, "MARKER old at %d end\n", i)
			fmt.Fprintf(&newB, "marker new at %d end\n", i)
		case 1:
			fmt.Fprintf(&oldB, "kept MARKER twice MARKER\n")
			fmt.Fprintf(&newB, "kept marker twice marker\n")
		default:
			fmt.Fprintf(&oldB, "filler line %d padding\n", i)
			fmt.Fprintf(&newB, "filler line %d padding\n", i)
		}
	}

	f := generate("f", Modified, oldB.String(), newB.String(), GenerateOptions{Context: 3}, 0)
	if !f.Paged() {
		t.Fatal("the fixture must be paged")
	}

	for _, term := range []string{"marker", "MARKER", "marker new", "line 42 padding", "old at 1"} {
		got, ok := f.MatchRows(term, 0, f.NLines())
		if !ok {
			t.Fatalf("%q: the source scan refused a plain ASCII term", term)
		}
		want := scanRendered(f, term)
		if len(got) != len(want) {
			t.Fatalf("%q: %d matches, want %d", term, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q match %d = %+v, want %+v", term, i, got[i], want[i])
			}
		}
	}
}

// TestPlanMatchesRange checks that a row range is respected, which is what the
// first search phase relies on: only the visible window is scanned.
func TestPlanMatchesRange(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&oldB, "line %d\n", i)
		if i%10 == 0 {
			fmt.Fprintf(&newB, "LINE %d changed\n", i)
			continue
		}
		fmt.Fprintf(&newB, "line %d\n", i)
	}

	f := generate("f", Modified, oldB.String(), newB.String(), GenerateOptions{Context: 3}, 0)
	all, ok := f.MatchRows("changed", 0, f.NLines())
	if !ok || len(all) == 0 {
		t.Fatalf("full scan found %d matches (ok=%v)", len(all), ok)
	}

	from, to := all[3].Row, all[8].Row
	part, ok := f.MatchRows("changed", from, to)
	if !ok {
		t.Fatal("range scan was refused")
	}
	var want []MatchRow
	for _, m := range all {
		if m.Row >= from && m.Row < to {
			want = append(want, m)
		}
	}
	if len(part) != len(want) {
		t.Fatalf("range scan found %d matches, want %d", len(part), len(want))
	}
	for i := range want {
		if part[i] != want[i] {
			t.Fatalf("range match %d = %+v, want %+v", i, part[i], want[i])
		}
	}
}

// TestPlanMatchesRefusesCheaply pins the cases that must fall back to scanning
// rendered lines: a term that could match the diff sign, and non-ASCII text.
func TestPlanMatchesRefusesCheaply(t *testing.T) {
	f := paged(t, "a\nb\n", "a\nc\n", GenerateOptions{Context: 3})
	for _, term := range []string{"", "+", "-x", " spaced", "naïve"} {
		if _, ok := f.MatchRows(term, 0, f.NLines()); ok {
			t.Fatalf("%q should not be answered by the source scan", term)
		}
	}
}

// TestPlanMatchesNeverStraddlesLines checks that a term containing a newline
// cannot match across two source lines.
func TestPlanMatchesNeverStraddlesLines(t *testing.T) {
	f := paged(t, "ab\n", "ab\ncd\n", GenerateOptions{Context: 3})
	got, ok := f.MatchRows("b\nc", 0, f.NLines())
	if !ok {
		t.Fatal("an ASCII term with a newline should still be scanned")
	}
	if len(got) != 0 {
		t.Fatalf("found %d matches spanning a line break: %+v", len(got), got)
	}
}
