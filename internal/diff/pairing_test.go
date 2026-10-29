package diff

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// oracleFile builds a file with known edits, so pairing can be scored exactly
// instead of judged: every line carries an id, a changed line keeps its id on
// the other side, a deleted line's id exists only on the old side, and an
// inserted line's only on the new side.
func oracleFile(n int) (oldText, newText string) {
	line := func(i, value int) string {
		return fmt.Sprintf("var v%03d = %d; // id%03d", i, value, i)
	}
	// The deleted and inserted lines are made of different words from the rest:
	// a bag-of-tokens matcher cannot tell apart lines that differ only in an
	// identifier, and an oracle that asked it to would be unfair.
	deletedLine := func(i int) string {
		return fmt.Sprintf("func teardownLegacy() { dropEverything() } // id%03d", i)
	}
	insertedLine := "func warmCacheNow() { primeEverything() } // idinserted"

	var oldB strings.Builder
	oldB.WriteString("package p\n\n")
	for i := 0; i < n; i++ {
		if i == n/3 {
			oldB.WriteString(deletedLine(i))
		} else {
			oldB.WriteString(line(i, i))
		}
		oldB.WriteString("\n")
	}

	var newB strings.Builder
	newB.WriteString("package p\n\n")
	for i := 0; i < n; i++ {
		switch {
		case i == n/3:
			// deleted: the id disappears from the new side
		case i%7 == 3:
			// changed: same id, different value
			newB.WriteString(line(i, i+1000))
			newB.WriteString("\n")
		default:
			newB.WriteString(line(i, i))
			newB.WriteString("\n")
		}
		if i == n/2 {
			// inserted: an id that exists only on the new side
			newB.WriteString(insertedLine)
			newB.WriteString("\n")
		}
	}
	return oldB.String(), newB.String()
}

// lineID extracts the trailing // id marker.
func lineID(text string) string {
	if i := strings.LastIndex(text, "// "); i >= 0 {
		return strings.TrimSpace(text[i+3:])
	}
	return ""
}

func hasID(lines []Line, idx []int, id string) bool {
	for _, i := range idx {
		if lineID(lines[i].Text) == id {
			return true
		}
	}
	return false
}

// TestPairingPrecisionRecall scores the pairing against the oracle the fixture
// constructs. A cross-pairing is a false positive, and an id present on both
// sides that stays unpaired is a false negative, so both must be zero.
func TestPairingPrecisionRecall(t *testing.T) {
	for _, n := range []int{8, 20, 60, 200} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			oldText, newText := oracleFile(n)
			f := Generate("a.go", Modified, oldText, newText, GenerateOptions{Context: 3})

			truePos, falsePos, missed := 0, 0, 0
			for _, h := range f.Hunks {
				for _, b := range changeBlocks(h.Lines) {
					if len(b.dels) == 0 || len(b.adds) == 0 {
						continue
					}
					pairedOld := map[string]bool{}
					for _, p := range pairRows(h.Lines, b.dels, b.adds) {
						oldID, newID := lineID(h.Lines[p[0]].Text), lineID(h.Lines[p[1]].Text)
						pairedOld[oldID] = true
						if oldID == newID {
							truePos++
							continue
						}
						falsePos++
						t.Logf("cross-paired %q with %q", h.Lines[p[0]].Text, h.Lines[p[1]].Text)
					}
					for _, i := range b.dels {
						id := lineID(h.Lines[i].Text)
						if !pairedOld[id] && hasID(h.Lines, b.adds, id) {
							missed++
						}
					}
				}
			}

			if falsePos != 0 || missed != 0 {
				t.Fatalf("pairs: %d correct, %d cross, %d missed", truePos, falsePos, missed)
			}
			if truePos == 0 {
				t.Fatal("no pairs produced")
			}
			t.Logf("pairs: %d correct, 0 cross, 0 missed", truePos)
		})
	}
}

// TestSimilarityComparesTokensNotPositions is the regression test for interned
// ids being assigned per line: two lines of the same shape but different
// content used to score 1.0, which is why unrelated rows got paired.
func TestSimilarityComparesTokensNotPositions(t *testing.T) {
	in := make(interner, 16)

	a := wordBag("var v002 = 2; // id002", in)
	b := wordBag("var v003 = 1003; // id003", in)
	if s := bagSimilarity(a, b); s >= 0.7 {
		t.Fatalf("different lines scored %.2f, ids are being compared by position", s)
	}

	c := wordBag("var v002 = 2; // id002", in)
	if s := bagSimilarity(a, c); s != 1 {
		t.Fatalf("identical lines scored %.2f, want 1", s)
	}
}

// TestOneGoodPairBeatsTwoWeakPairs is the regression test for the alignment
// objective: without a per-pair cost, pairing two mediocre rows outbids pairing
// the one row that actually corresponds, and both wrong rows get highlighted.
func TestOneGoodPairBeatsTwoWeakPairs(t *testing.T) {
	lines := []Line{
		{Kind: Deletion, Text: "-var v002 = 2; // id002"},
		{Kind: Deletion, Text: "-var v003 = 3; // id003"},
		{Kind: Addition, Text: "+var v003 = 1003; // id003"},
		{Kind: Addition, Text: "+var extra = 0; // idinserted"},
	}

	pairs := pairRows(lines, []int{0, 1}, []int{2, 3})
	if len(pairs) != 1 || pairs[0] != [2]int{1, 2} {
		t.Fatalf("pairs = %v, want [[1 2]]: the matching rows, not the two weak ones", pairs)
	}
}
