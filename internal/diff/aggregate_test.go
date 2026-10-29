package diff

import "testing"

func aggregateFixture(path string, extra int) *File {
	old := "one\ntwo\nthree\n"
	new := "one\nTWO\nthree\n"
	for range extra {
		old += "tail\n"
		new += "tail\n"
	}
	return Generate(path, Modified, old, new, GenerateOptions{Context: 3})
}

func TestAppendMatchesAggregate(t *testing.T) {
	files := []*File{
		aggregateFixture("a.go", 0),
		aggregateFixture("b.go", 5),
		aggregateFixture("dir/c.go", 2),
	}

	want := Aggregate(files)
	got := &File{Kind: Modified}
	for _, f := range files {
		got.Append(f)
	}

	if len(got.Lines) != len(want.Lines) {
		t.Fatalf("lines = %d, want %d", len(got.Lines), len(want.Lines))
	}
	for i := range want.Lines {
		g, w := got.Lines[i], want.Lines[i]
		if g.Kind != w.Kind || g.Text != w.Text || g.Old != w.Old || g.New != w.New {
			t.Fatalf("line %d = %+v, want %+v", i, g, w)
		}
	}
	if len(got.Hunks) != len(want.Hunks) {
		t.Fatalf("hunks = %d, want %d", len(got.Hunks), len(want.Hunks))
	}
	for i := range want.Hunks {
		if got.Hunks[i].LineIndex != want.Hunks[i].LineIndex {
			t.Fatalf("hunk %d at %d, want %d", i, got.Hunks[i].LineIndex, want.Hunks[i].LineIndex)
		}
	}
	if got.Additions != want.Additions || got.Deletions != want.Deletions {
		t.Fatalf("stats = +%d -%d, want +%d -%d", got.Additions, got.Deletions, want.Additions, want.Deletions)
	}
}

func TestAppendKeepsExistingPositions(t *testing.T) {
	first := aggregateFixture("a.go", 4)

	doc := &File{Kind: Modified}
	doc.Append(first)

	linesBefore := len(doc.Lines)
	firstLine := doc.Lines[0].Text
	hunksBefore := append([]Hunk(nil), doc.Hunks...)

	doc.Append(aggregateFixture("b.go", 3))

	if len(doc.Lines) <= linesBefore {
		t.Fatal("appending a second file did not grow the document")
	}
	if doc.Lines[0].Text != firstLine {
		t.Fatal("the first line moved")
	}
	for i, h := range hunksBefore {
		if doc.Hunks[i].LineIndex != h.LineIndex {
			t.Fatalf("hunk %d moved from %d to %d", i, h.LineIndex, doc.Hunks[i].LineIndex)
		}
	}
	if doc.Hunks[len(hunksBefore)].LineIndex < linesBefore {
		t.Fatal("appended hunks should point at the appended block")
	}
}
