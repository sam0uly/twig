package diffview

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
)

func TestReadSelectionCopiesRows(t *testing.T) {
	m := newSized(false, 60)
	m.StartReadSelection(2, 0)
	m.ExtendReadSelection(4, 1000)
	text, ok := m.FinishReadSelection()
	if !ok {
		t.Fatal("drag did not finish as a selection")
	}
	if lines := strings.Count(text, "\n") + 1; lines != 3 {
		t.Fatalf("copied %d lines, want 3: %q", lines, text)
	}
	if text == "" {
		t.Fatal("selection copied no text")
	}
	if m.ReadSelectionActive() {
		t.Fatal("selection stayed highlighted after copying")
	}
}

func TestReadSelectionNeedsADrag(t *testing.T) {
	m := newSized(false, 60)
	m.StartReadSelection(3, 0)
	if _, ok := m.FinishReadSelection(); ok {
		t.Fatal("a click without dragging copied a selection")
	}
}

func TestReadSelectionCopiesExactCharacters(t *testing.T) {
	m := newSized(false, 60)
	row := 2
	left := m.barWidth() + m.gut.cells() + m.foldMarkerWidth(row)
	m.StartReadSelection(row, left)
	m.ExtendReadSelection(row, left+3)
	text, ok := m.FinishReadSelection()
	if !ok {
		t.Fatal("character drag did not finish")
	}
	want := m.readRowText(m.logicalAt(row))
	want = want[:columnByte(want, 3)]
	if want != "" && strings.ContainsRune("+- ", rune(want[0])) {
		want = want[1:]
	}
	if text != want {
		t.Fatalf("selected text = %q, want %q", text, want)
	}
}

func TestReadSelectionHighlightsSelectedLetters(t *testing.T) {
	m := newSized(false, 60)
	row := rowOfKind(m, diff.Addition)
	left := m.barWidth() + m.gut.cells() + m.foldMarkerWidth(row)
	text := m.readRowText(m.logicalAt(row))
	if len([]byte(text)) < 5 {
		t.Fatalf("test row is too short: %q", text)
	}
	m.paintRow(row)
	m.StartReadSelection(row, left+5)
	m.ExtendReadSelection(row, left+8)
	selected := m.SelectionText()
	if strings.TrimSpace(selected) == "" {
		t.Fatalf("selected no visible letters from %q", text)
	}
	style := m.styles.Selection()
	const probe = "\x00probe\x00"
	prefix := style.Render(probe)
	prefix = prefix[:strings.Index(prefix, probe)]
	if got := m.paintRow(row); !strings.Contains(got, prefix) {
		t.Fatalf("selected letters are not highlighted: %q", got)
	}
}

// TestReadSelectionLeavesTheSignAlone: the diff sign is not part of the text a
// drag copies, so it must not be part of the highlight either.
func TestReadSelectionLeavesTheSignAlone(t *testing.T) {
	whole := newSized(false, 60)
	row := rowOfKind(whole, diff.Addition)
	text := whole.readRowText(whole.logicalAt(row))
	if text == "" || !strings.ContainsRune("+- ", rune(text[0])) {
		t.Fatalf("row %d is not a signed diff line: %q", row, text)
	}
	left := whole.barWidth() + whole.gut.cells() + whole.foldMarkerWidth(row)
	whole.StartReadSelection(row, left)
	whole.ExtendReadSelection(row, left+1000)

	// Selecting from the row's left edge lands exactly where selecting from the
	// first text column does.
	after := newSized(false, 60)
	after.StartReadSelection(row, left+1)
	after.ExtendReadSelection(row, left+1000)

	if got, want := whole.paintRow(row), after.paintRow(row); got != want {
		t.Fatalf("the sign is highlighted:\nwhole %q\nafter %q", got, want)
	}
	if sel := whole.SelectionText(); strings.HasPrefix(sel, string(text[0])) {
		t.Fatalf("copied text kept the sign: %q", sel)
	}
}

func TestReadSelectionStaysInItsSplitPane(t *testing.T) {
	m := newSized(true, 60)
	row := -1
	for i, r := range m.rows {
		if r.hasLeft && r.hasRight && r.left.Kind == diff.Deletion && r.right.Kind == diff.Addition {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("sample has no paired split row")
	}

	markerW := m.foldMarkerWidth(row)
	leftW, _ := splitColumnWidths(m.rowBudget() - markerW)
	leftBase := m.barWidth() + markerW + m.gut.numW + 1
	m.StartReadSelection(row, leftBase)
	m.ExtendReadSelection(row, 1000)
	left := m.SelectionText()
	if !strings.Contains(left, "old()") || strings.Contains(left, "new()") {
		t.Fatalf("left drag escaped its pane: %q", left)
	}

	rightBase := m.barWidth() + markerW + leftW + 1 + m.gut.numW + 1
	m.StartReadSelection(row, rightBase)
	m.ExtendReadSelection(row, 1000)
	right := m.SelectionText()
	if !strings.Contains(right, "new()") || strings.Contains(right, "old()") {
		t.Fatalf("right drag selected the other pane: %q", right)
	}
}
