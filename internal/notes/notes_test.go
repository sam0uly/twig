package notes

import "testing"

func TestAValidNoteOfEachKind(t *testing.T) {
	for _, note := range []Note{
		{Kind: File, Path: "internal/ui/tui.go", Body: "read this first"},
		{Kind: Hunk, Path: "internal/ui/tui.go", Hunk: 2, Body: "this hunk hides the cursor"},
		{Kind: Lines, Path: "internal/ui/tui.go", Start: 120, End: 124, Body: "same bug as above"},
		{Kind: Lines, Path: "internal/ui/tui.go", Start: 120, End: 120, Body: "one line"},
	} {
		if err := note.Validate(); err != nil {
			t.Errorf("%s note %+v: %v", note.Kind, note, err)
		}
	}
}

func TestAFullNote(t *testing.T) {
	for _, note := range []Note{
		{Kind: File, Path: "a.go"},
		{Kind: Hunk, Path: "a.go", Hunk: 1},
		{Kind: Lines, Path: "a.go"},
		{Kind: Hunk},
		{Kind: Lines, Path: "a.go", Start: 3, End: 2},
		{Kind: 9, Path: "a.go", Body: "x"},
	} {
		if err := note.Validate(); err == nil {
			t.Errorf("incomplete %s note %+v was accepted", note.Kind, note)
		}
	}
}

func TestNoteFieldsMatchTheirKind(t *testing.T) {
	for _, note := range []Note{
		{Kind: File, Path: "a.go", Body: "x", Hunk: 1},
		{Kind: File, Path: "a.go", Body: "x", Start: 1, End: 1},
		{Kind: Hunk, Path: "a.go", Body: "x", Hunk: 1, Start: 1, End: 1},
		{Kind: Lines, Path: "a.go", Body: "x", Start: 1, End: 1, Hunk: 1},
	} {
		if err := note.Validate(); err == nil {
			t.Errorf("%s note %+v carries fields of another kind", note.Kind, note)
		}
	}
}

func TestKindString(t *testing.T) {
	for kind, want := range map[Kind]string{File: "file", Hunk: "hunk", Lines: "lines", 7: "unknown"} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d) = %q, want %q", kind, got, want)
		}
	}
}

func TestTags(t *testing.T) {
	for tag, want := range map[Tag]string{Fyi: "fyi", Todo: "todo", Warn: "warn", Error: "error"} {
		if got := tag.String(); got != want {
			t.Errorf("Tag(%d) = %q, want %q", tag, got, want)
		}
	}
	for arg, want := range map[string]Tag{
		"":        Fyi,
		"note":    Fyi,
		"FYI":     Fyi,
		"todo":    Todo,
		"FIXME":   Todo,
		"warning": Warn,
		"ERR":     Error,
		"oops":    Error,
	} {
		got, err := ParseTag(arg)
		if err != nil {
			t.Fatalf("ParseTag(%q): %v", arg, err)
		}
		if got != want {
			t.Errorf("ParseTag(%q) = %v, want %v", arg, got, want)
		}
	}
	if _, err := ParseTag("nope"); err == nil {
		t.Error("ParseTag accepted an unknown tag")
	}

	tag := Fyi
	for range TagCount {
		tag = tag.Next()
	}
	if tag != Fyi {
		t.Fatalf("Next did not wrap around: %v", tag)
	}
	if Fyi.Next() != Todo {
		t.Fatalf("Fyi.Next() = %v, want Todo", Fyi.Next())
	}
}

func TestSplitTag(t *testing.T) {
	tests := []struct {
		body string
		tag  Tag
		want string
	}{
		{"fix this", Fyi, "fix this"},
		{"todo: fix this", Todo, "fix this"},
		{"TODO fix this", Todo, "fix this"},
		{"warn the pointer", Warn, "the pointer"},
		{"error: nil map", Error, "nil map"},
		{"oops", Error, ""},
		{"note that the value is nil", Fyi, "note that the value is nil"},
		{"  todo:   spaced  ", Todo, "spaced"},
	}
	for _, tt := range tests {
		tag, body := SplitTag(tt.body)
		if tag != tt.tag || body != tt.want {
			t.Errorf("SplitTag(%q) = (%v, %q), want (%v, %q)", tt.body, tag, body, tt.tag, tt.want)
		}
	}
}
