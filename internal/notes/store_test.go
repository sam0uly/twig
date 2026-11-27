package notes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		arg  string
		want Note
	}{
		{"a.go", Note{Kind: File, Path: "a.go"}},
		{"a.go:2", Note{Kind: Lines, Path: "a.go", Start: 2, End: 2}},
		{"a.go:2-4", Note{Kind: Lines, Path: "a.go", Start: 2, End: 4}},
		{"a.go#3", Note{Kind: Hunk, Path: "a.go", Hunk: 3}},
		{"dir:name/a.go:7", Note{Kind: Lines, Path: "dir:name/a.go", Start: 7, End: 7}},
		{"a.go/", Note{Kind: File, Path: "a.go"}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.arg)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.arg, err)
		}
		if got.Kind != tt.want.Kind || got.Path != tt.want.Path || got.Start != tt.want.Start || got.End != tt.want.End || got.Hunk != tt.want.Hunk {
			t.Fatalf("Parse(%q) = %+v, want %+v", tt.arg, got, tt.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, arg := range []string{"", ":2", "a.go:0", "a.go:2-1", "a.go:2-x", "a.go#0", "a.go#x"} {
		if _, err := Parse(arg); err == nil {
			t.Errorf("Parse(%q) was accepted", arg)
		}
	}
}

func TestAnchorString(t *testing.T) {
	for _, note := range []Note{
		{Kind: File, Path: "a.go"},
		{Kind: Lines, Path: "a.go", Start: 2, End: 2},
		{Kind: Lines, Path: "a.go", Start: 2, End: 4},
		{Kind: Hunk, Path: "a.go", Hunk: 3},
	} {
		again, err := Parse(note.Anchor())
		if err != nil {
			t.Fatalf("Parse(%q): %v", note.Anchor(), err)
		}
		if again.Kind != note.Kind || again.Path != note.Path || again.Start != note.Start || again.End != note.End || again.Hunk != note.Hunk {
			t.Fatalf("round trip of %q gave %+v", note.Anchor(), again)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twig-notes.json")

	empty, err := Load(path)
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if len(empty.Notes) != 0 || empty.NextID != 1 {
		t.Fatalf("empty store = %+v", empty)
	}

	store := empty
	first := store.Add(Note{Kind: Lines, Path: "a.go", Start: 2, End: 2, Body: "one"})
	second := store.Add(Note{Kind: Hunk, Path: "a.go", Hunk: 1, Body: "two"})
	if first.ID != "n1" || second.ID != "n2" {
		t.Fatalf("ids = %q %q", first.ID, second.ID)
	}
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.NextID != 3 || len(loaded.Notes) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if !loaded.Has("n1") || loaded.Has("n9") {
		t.Fatal("Has is wrong")
	}
	if got := loaded.For("a.go"); len(got) != 2 {
		t.Fatalf("For(a.go) = %d notes", len(got))
	}
	if got := loaded.Remove("n1"); got != 1 {
		t.Fatalf("Remove removed %d", got)
	}
	if got := loaded.Clear("a.go"); got != 1 {
		t.Fatalf("Clear removed %d", got)
	}
	if len(loaded.Notes) != 0 {
		t.Fatalf("notes left = %+v", loaded.Notes)
	}
}

func TestResolveAndDrift(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "one\ntwo\r\nthree\n")

	note := Note{Kind: Lines, Path: "a.go", Start: 2, End: 3, Body: "x"}
	if err := note.Resolve(root); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(note.Text) != 2 || note.Text[0] != "two" || note.Text[1] != "three" {
		t.Fatalf("text = %q", note.Text)
	}
	if note.Drifted(root) {
		t.Fatal("a fresh note reads as drifted")
	}

	writeFile(t, root, "a.go", "zero\none\ntwo\nthree\n")
	if note.Drifted(root) {
		t.Fatal("text that only moved reads as drifted")
	}

	writeFile(t, root, "a.go", "one\nCHANGED\nthree\n")
	if !note.Drifted(root) {
		t.Fatal("an edited line did not drift")
	}

	past := Note{Kind: Lines, Path: "a.go", Start: 9, End: 9, Body: "x"}
	if err := past.Resolve(root); err == nil {
		t.Fatal("a line past the end was accepted")
	}

	fileNote := Note{Kind: File, Path: "a.go", Body: "x"}
	if err := fileNote.Resolve(root); err != nil || fileNote.Drifted(root) {
		t.Fatalf("file note: %v, drifted=%v", err, fileNote.Drifted(root))
	}
	gone := Note{Kind: File, Path: "missing.go", Body: "x"}
	if !gone.Drifted(root) {
		t.Fatal("a note on a gone file did not drift")
	}
}

func TestHunkDrift(t *testing.T) {
	note := Note{Kind: Hunk, Path: "a.go", Hunk: 2, Header: "@@ -1 +1 @@"}
	if note.HunkDrifted("@@ -1 +1 @@", 2) {
		t.Fatal("an unchanged hunk reads as drifted")
	}
	if !note.HunkDrifted("@@ -9 +9 @@", 2) {
		t.Fatal("a moved header did not drift")
	}
	if !note.HunkDrifted("@@ -1 +1 @@", 3) {
		t.Fatal("a moved hunk number did not drift")
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
