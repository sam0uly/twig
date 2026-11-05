package diffview

import (
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/ui/styles"
)

// changeLines names a handful of changed lines, one per hunk, which is what a follow
// is given: the line a diff says changed.
func changeLines(f *diff.File) []diff.Line {
	var out []diff.Line
	var add, del bool
	for i := 0; i < len(f.Hunks); i++ {
		h, ok := f.HunkAt(i)
		if !ok {
			continue
		}
		add, del = false, false
		for _, l := range h.Lines {
			switch {
			case l.Kind == diff.Addition && !add:
				out, add = append(out, l), true
			case l.Kind == diff.Deletion && !del:
				out, del = append(out, l), true
			}
		}
	}
	return out
}

// paints reports whether a row paints the line the lookup was given.
func paints(m *Model, row int, line diff.Line) bool {
	return m.eachLine(row, func(l diff.Line) bool {
		return l.Kind == line.Kind && l.Text == line.Text &&
			(line.Kind == diff.Addition && l.New == line.New ||
				line.Kind == diff.Deletion && l.Old == line.Old)
	})
}

// TestRowOfChangeMatchesTheFlatLayout: the follow places its cursor from the diff's
// line numbers instead of walking the rows, so the row it computes has to be the row
// the flat layout paints. The fixture stays resident, since the flat layout holds every
// row and is the reference the virtual one has to match.
func TestRowOfChangeMatchesTheFlatLayout(t *testing.T) {
	f := changedFile(3_000, 9)
	if f.Paged() {
		t.Fatal("the fixture must stay resident to be a reference")
	}
	flat := newFlatView(f, 120, 24)
	virt := newVirtualView(f, 120, 24)

	lines := changeLines(f)
	if len(lines) < 10 {
		t.Fatalf("%d change lines, want a document with many", len(lines))
	}
	for _, line := range lines {
		want, ok := flat.matchRow("", line, true)
		if !ok {
			t.Fatalf("the flat layout has no row for %+v", line)
		}
		got, ok := virt.rowOfChange("", line)
		if !ok {
			t.Fatalf("the lookup found no row for %+v", line)
		}
		if got != want {
			t.Fatalf("line %+v: lookup row %d, flat row %d", line, got, want)
		}
	}
}

// TestRowOfChangeInAnAggregate: a document of many files holds a block per file, and
// the rows of a block start after its band. A lookup that forgets the band lands a
// row short, which reads as the cursor resting on the line before the change.
func TestRowOfChangeInAnAggregate(t *testing.T) {
	files := []*diff.File{
		docTestFile("a.txt", 6, "FIRST"),
		changedFile(20_000, 10),
		docTestFile("c.txt", 6, "LAST"),
	}
	m := New(false, styles.New())
	m.SetSize(120, 24)
	m.SetDocument(func(i int) *diff.File { return files[i] })
	m.AppendBlocksSided(files, make([]BlockSide, len(files)))

	for _, line := range changeLines(files[1]) {
		row, ok := m.rowOfChange(files[1].Path(), line)
		if !ok {
			t.Fatalf("the lookup found no row for %+v", line)
		}
		if !paints(&m, row, line) {
			t.Fatalf("row %d does not paint %+v", row, line)
		}
	}
}

// TestFollowEditLandsOnTheChange: the whole point of the lookup is that following an
// edit in a paged document places the cursor on it, which a scan cannot do because
// the rows are not there to scan.
func TestFollowEditLandsOnTheChange(t *testing.T) {
	f := changedFile(20_000, 10)
	m := newVirtualView(f, 120, 24)
	m.SetSelectable(true)

	lines := changeLines(f)
	last := lines[len(lines)-1]
	if !m.FollowEdit(last) {
		t.Fatalf("the follow did not place the cursor on %+v", last)
	}
	if !paints(&m, m.CursorRow(), last) {
		t.Fatalf("the cursor is on row %d, not on %+v", m.CursorRow(), last)
	}
}

// TestRowOfChangeKeepsWorkingWhenWrapped: soft wrapping turns a row into several
// display lines, not a line into several rows, so the lookup still answers. Falling
// back to the scan there costs the whole document on a paged diff, and finds nothing.
func TestRowOfChangeKeepsWorkingWhenWrapped(t *testing.T) {
	f := changedFile(20_000, 10)
	m := newVirtualView(f, 120, 24)
	m.SetWrap(true)
	if !m.vp.SoftWrap {
		t.Fatal("the fixture is not wrapped")
	}

	lines := changeLines(f)
	last := lines[len(lines)-1]
	row, ok := m.rowOfChange("", last)
	if !ok {
		t.Fatalf("the lookup gave up on a wrapped document for %+v", last)
	}
	m.rowAt(row)
	if !paints(&m, row, last) {
		t.Fatalf("row %d does not paint %+v", row, last)
	}
}

// TestRowOfChangeStandsDownWhenPaired: split mode draws two lines on one row, so a
// line number no longer names a row and the scan has to pick it.
func TestRowOfChangeStandsDownWhenPaired(t *testing.T) {
	f := changedFile(3_000, 9)
	m := newFlatView(f, 120, 24)
	m.SetSplit(true)
	if _, ok := m.rowOfChange("", changeLines(f)[0]); ok {
		t.Fatal("the lookup answered for paired rows")
	}
}
