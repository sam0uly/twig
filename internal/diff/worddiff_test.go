package diff

import (
	"fmt"
	"strings"
	"testing"
)

func spansEqual(got, want []Span) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestWordDiffSingleWordChange(t *testing.T) {
	t.Parallel()

	oldSpans, newSpans := changedSpans("hello beautiful world", "hello awesome world")
	if !spansEqual(oldSpans, []Span{{6, 15}}) {
		t.Errorf("old spans = %v, want [{6 15}]", oldSpans)
	}
	if !spansEqual(newSpans, []Span{{6, 13}}) {
		t.Errorf("new spans = %v, want [{6 13}]", newSpans)
	}
}

func TestWordDiffIdentical(t *testing.T) {
	t.Parallel()

	oldSpans, newSpans := changedSpans("same line", "same line")
	if oldSpans != nil || newSpans != nil {
		t.Errorf("identical strings produced spans %v / %v", oldSpans, newSpans)
	}
}

func TestWordDiffWhitespaceChange(t *testing.T) {
	t.Parallel()

	oldSpans, newSpans := changedSpans("a b", "a  b")
	if !spansEqual(oldSpans, []Span{{1, 2}}) {
		t.Errorf("old spans = %v, want [{1 2}]", oldSpans)
	}
	if !spansEqual(newSpans, []Span{{1, 3}}) {
		t.Errorf("new spans = %v, want [{1 3}]", newSpans)
	}
}

func TestWordDiffPrefixSuffixTrim(t *testing.T) {
	t.Parallel()

	oldSpans, newSpans := changedSpans("start middleA end", "start middleB end")
	if !spansEqual(oldSpans, []Span{{6, 13}}) {
		t.Errorf("old spans = %v", oldSpans)
	}
	if !spansEqual(newSpans, []Span{{6, 13}}) {
		t.Errorf("new spans = %v", newSpans)
	}
}

func TestWordDiffLargeInputStaysPrecise(t *testing.T) {
	t.Parallel()

	// 300 changed words per side: the engine must keep matching the shared
	// separators instead of giving up and marking the whole line changed.
	var longA, longB strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&longA, "a%d ", i)
		fmt.Fprintf(&longB, "b%d ", i)
	}
	a, b := longA.String(), longB.String()
	oldSpans, newSpans := changedSpans(a, b)

	if len(oldSpans) != 300 || len(newSpans) != 300 {
		t.Fatalf("spans = %d/%d, want one per word (300/300)", len(oldSpans), len(newSpans))
	}
	if want := (Span{0, 2}); oldSpans[0] != want {
		t.Errorf("first old span = %v, want %v", oldSpans[0], want)
	}
	if want := (Span{len(a) - 5, len(a) - 1}); oldSpans[299] != want {
		t.Errorf("last old span = %v, want %v", oldSpans[299], want)
	}
	if want := (Span{len(b) - 5, len(b) - 1}); newSpans[299] != want {
		t.Errorf("last new span = %v, want %v", newSpans[299], want)
	}
}

func TestPairRowsHomologous(t *testing.T) {
	t.Parallel()

	// A deletion run of three rows against an addition run whose first row
	// was inserted: pairing by index would compare unrelated lines, pairing
	// by similarity keeps each rewrite attached to its own row.
	lines := []Line{
		{Kind: Deletion, Text: "-const answer = 41"},
		{Kind: Deletion, Text: "-func main() { run() }"},
		{Kind: Addition, Text: "+// a brand new comment line"},
		{Kind: Addition, Text: "+const answer = 42"},
		{Kind: Addition, Text: "+func main() { go run() }"},
	}
	pairs := pairRows(lines, []int{0, 1}, []int{2, 3, 4})
	if len(pairs) != 2 {
		t.Fatalf("pairs = %v, want 2", pairs)
	}
	if pairs[0] != [2]int{0, 3} || pairs[1] != [2]int{1, 4} {
		t.Fatalf("pairs = %v, want [[0 3] [1 4]]", pairs)
	}
}

func TestAnnotateWordDiffOnHunks(t *testing.T) {
	t.Parallel()

	got := Parse(mustParseFile(t, "worddiff.diff"))
	f := got.Files[0]

	// The hunk changes "const greeting = 'hello'" to "const greeting = 'hi'",
	// so only the word "hello"/"hi" should be marked.
	var del, add Line
	for _, l := range f.Lines {
		switch l.Kind {
		case Deletion:
			del = l
		case Addition:
			add = l
		}
	}

	if del.Text == "" {
		t.Fatal("no deletion line found")
	}

	// Words are split from punctuation, so the quotes and the semicolon are
	// shared and only the word inside them changes.
	if !spansEqual(del.WordRanges(), []Span{{21, 26}}) {
		t.Errorf("del ranges = %v, want [{21 26}] (text: %q)", del.WordRanges(), del.Text)
	}
	if !spansEqual(add.WordRanges(), []Span{{21, 23}}) {
		t.Errorf("add ranges = %v, want [{21 23}] (text: %q)", add.WordRanges(), add.Text)
	}

	// Context lines must never carry word ranges.
	for _, l := range f.Lines {
		if l.Kind == Context && l.WordRanges() != nil {
			t.Errorf("context line %q has ranges %v", l.Text, l.WordRanges())
		}
	}
}

func TestWordDiffTokenMatchWalk(t *testing.T) {
	t.Parallel()

	// Reordering across many tokens must still find matches, not mark
	// everything changed.
	oldSpans, newSpans := changedSpans("a b c d e", "a c d b e")
	if len(oldSpans) > 4 {
		t.Errorf("old spans = %v, expected a bounded set", oldSpans)
	}
	if len(newSpans) > 4 {
		t.Errorf("new spans = %v, expected a bounded set", newSpans)
	}
}

// TestChangedSpansSplitPunctuation pins the rule that words are separated from
// punctuation: a change inside a string literal marks the word, not the whole
// call expression and not the quotes.
func TestChangedSpansSplitPunctuation(t *testing.T) {
	t.Parallel()

	oldText := `printSuccess("added %s (%s %s)", name)`
	newText := `printSuccess("addedo %s (%s %s)", name)`
	oldSpans, newSpans := changedSpans(oldText, newText)

	if !spansEqual(oldSpans, []Span{{14, 19}}) {
		t.Errorf("old spans = %v, want [{14 19}] (%q)", oldSpans, oldText[14:19])
	}
	if !spansEqual(newSpans, []Span{{14, 20}}) {
		t.Errorf("new spans = %v, want [{14 20}] (%q)", newSpans, newText[14:20])
	}

	// A punctuation change marks the punctuation itself.
	opOld, opNew := changedSpans("x := a + b", "x := a - b")
	if !spansEqual(opOld, []Span{{7, 8}}) {
		t.Errorf("operator old spans = %v, want the '+'", opOld)
	}
	if !spansEqual(opNew, []Span{{7, 8}}) {
		t.Errorf("operator new spans = %v, want the '-'", opNew)
	}
}
