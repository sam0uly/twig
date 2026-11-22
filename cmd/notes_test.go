package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runNotes(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	// A config of its own: the reader's context decides the hunks the listing
	// prints, and CI has none.
	t.Setenv("TWIG_CONFIG", notesConfig(t))
	var out bytes.Buffer
	c := New()
	c.SetArgs(args)
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	err := c.Execute()
	return out.String(), err
}

// notesConfig is a whole-file context, so the fixtures read as one hunk.
func notesConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "twig.json")
	if err := os.WriteFile(path, []byte("{\"context\": 10}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func notesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n")
	write("sub/c.go", "one\ntwo\n")
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)
		cmd := exec.Command("git", full...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", ".")
	git("add", ".")
	git("commit", "-qm", "base")
	write("a.go", "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n")
	write("sub/c.go", "one\nTWO\n")
	t.Chdir(dir)
	return dir
}

func TestNotesAddListAndRemove(t *testing.T) {
	notesRepo(t)

	if out, err := runNotes(t, "", "notes", "add", "a.go", "-m", "file level"); err != nil || strings.TrimSpace(out) != "n1" {
		t.Fatalf("add file = %q, %v", out, err)
	}
	if out, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "the line"); err != nil || strings.TrimSpace(out) != "n2" {
		t.Fatalf("add line = %q, %v", out, err)
	}
	if out, err := runNotes(t, "", "notes", "add", "a.go#1", "-m", "the hunk"); err != nil || strings.TrimSpace(out) != "n3" {
		t.Fatalf("add hunk = %q, %v", out, err)
	}

	out, err := runNotes(t, "", "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"n1  a.go  file level", "n2  a.go:2  the line", "n3  a.go#1  the hunk"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list is missing %q:\n%s", want, out)
		}
	}

	if _, err := runNotes(t, "", "notes", "rm", "n2"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	out, _ = runNotes(t, "", "notes")
	if strings.Contains(out, "n2 ") {
		t.Fatalf("n2 survived rm:\n%s", out)
	}
	if _, err := runNotes(t, "", "notes", "rm", "n99"); err == nil {
		t.Fatal("rm of a missing id was accepted")
	}
}

func TestNotesJSON(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go:2-3", "-m", "with <html> & \"quotes\""); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runNotes(t, "", "notes", "--json")
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var doc struct {
		V     int `json:"v"`
		Notes []struct {
			ID      string   `json:"id"`
			Kind    string   `json:"kind"`
			Anchor  string   `json:"anchor"`
			Path    string   `json:"path"`
			Start   int      `json:"start"`
			End     int      `json:"end"`
			Drifted bool     `json:"drifted"`
			Text    []string `json:"text"`
			Body    string   `json:"body"`
		} `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, out)
	}
	if doc.V != 1 || len(doc.Notes) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	note := doc.Notes[0]
	if note.Kind != "lines" || note.Anchor != "a.go:2-3" || note.Start != 2 || note.End != 3 {
		t.Fatalf("note = %+v", note)
	}
	if note.Drifted {
		t.Fatal("a fresh note reads as drifted")
	}
	if len(note.Text) != 2 || note.Text[0] != "TWO" {
		t.Fatalf("text = %q", note.Text)
	}
	if !strings.Contains(out, "<html>") {
		t.Fatalf("html was escaped:\n%s", out)
	}
}

func TestNotesDriftAfterAnEdit(t *testing.T) {
	dir := notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "the line"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\nCHANGED\nthree\nfour\nfive\nsix\nseven\neight\nnine\nTEN\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runNotes(t, "", "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "(drifted)") {
		t.Fatalf("the edited line did not drift:\n%s", out)
	}
}

func TestNotesFromASubdirectory(t *testing.T) {
	dir := notesRepo(t)
	if err := os.Chdir(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if _, err := runNotes(t, "", "notes", "add", "c.go:2", "-m", "from subdir"); err != nil {
		t.Fatalf("add: %v", err)
	}
	out, _ := runNotes(t, "", "notes")
	if !strings.Contains(out, "sub/c.go:2") {
		t.Fatalf("the path was not made relative to the root:\n%s", out)
	}
}

func TestNotesBodyFromStdin(t *testing.T) {
	notesRepo(t)
	if out, err := runNotes(t, "piped body\n", "notes", "add", "a.go:2"); err != nil || strings.TrimSpace(out) != "n1" {
		t.Fatalf("add from stdin = %q, %v", out, err)
	}
	out, _ := runNotes(t, "", "notes")
	if !strings.Contains(out, "piped body") {
		t.Fatalf("the piped body was not stored:\n%s", out)
	}
}

// The listing carries the code around each note; --brief does not.
func TestNotesShowTheirCodeContext(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "the line"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runNotes(t, "", "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"→ ", "-two", "+TWO"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the context is missing %q:\n%s", want, out)
		}
	}

	brief, err := runNotes(t, "", "notes", "--brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if strings.Contains(brief, "→") || strings.Contains(brief, "+TWO") {
		t.Fatalf("--brief still prints context:\n%s", brief)
	}
	if !strings.Contains(brief, "the line") {
		t.Fatalf("--brief lost the note itself:\n%s", brief)
	}

	js, err := runNotes(t, "", "notes", "--json")
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(js, `"context":[`) || !strings.Contains(js, "→ ") {
		t.Fatalf("the json has no context:\n%s", js)
	}
}

// Two notes on one anchor are two lines: the code is printed once.
func TestNotesOnOneAnchorShareTheirContext(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "second"); err != nil {
		t.Fatal(err)
	}

	out, err := runNotes(t, "", "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := strings.Count(out, "→ "); got != 1 {
		t.Fatalf("the anchored row is marked %d times:\n%s", got, out)
	}
	if got := strings.Count(out, "-two"); got != 1 {
		t.Fatalf("the context is printed %d times:\n%s", got, out)
	}
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the listing lost %q:\n%s", want, out)
		}
	}
	// Both notes come first, then their one block of code.
	second := strings.Index(out, "second")
	if code := strings.Index(out, "→ "); second > code {
		t.Fatalf("a note is listed below the code:\n%s", out)
	}
}

func TestNotesTag(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go:2", "-m", "fix this", "--tag", "warn"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, _ := runNotes(t, "", "notes")
	if !strings.Contains(out, "WARN ") || !strings.Contains(out, "fix this") {
		t.Fatalf("the tag is not in the list:\n%s", out)
	}
	js, _ := runNotes(t, "", "notes", "--json")
	if !strings.Contains(js, `"tag":"warn"`) {
		t.Fatalf("the tag is not in the json:\n%s", js)
	}
	if _, err := runNotes(t, "", "notes", "add", "a.go:3", "-m", "x", "--tag", "nope"); err == nil {
		t.Fatal("an unknown tag was accepted")
	}

	// A tag word sets the tag and leaves the body.
	if _, err := runNotes(t, "", "notes", "add", "a.go:4", "-m", "error: the map is nil"); err != nil {
		t.Fatalf("add with a prefix: %v", err)
	}
	js, _ = runNotes(t, "", "notes", "--json")
	if !strings.Contains(js, `"tag":"error"`) || !strings.Contains(js, `"body":"the map is nil"`) {
		t.Fatalf("the prefix tag did not stick:\n%s", js)
	}

	// An explicit --tag wins.
	if _, err := runNotes(t, "", "notes", "add", "a.go:5", "-m", "todo: keep me", "--tag", "warn"); err != nil {
		t.Fatalf("add with both: %v", err)
	}
	js, _ = runNotes(t, "", "notes", "--json")
	if !strings.Contains(js, `"tag":"warn"`) || !strings.Contains(js, `"body":"todo: keep me"`) {
		t.Fatalf("--tag did not win:\n%s", js)
	}
}

func TestNotesRefusals(t *testing.T) {
	notesRepo(t)
	for _, arg := range []string{"a.go:99", "a.go:0", "a.go:2-1", "a.go#9", "missing.go"} {
		if _, err := runNotes(t, "", "notes", "add", arg, "-m", "x"); err == nil {
			t.Errorf("add %q was accepted", arg)
		}
	}
	if _, err := runNotes(t, "", "notes", "add", "a.go"); err == nil {
		t.Error("a note without a body was accepted")
	}
}

func TestNotesOutsideARepository(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := runNotes(t, "", "notes"); err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("outside a repository = %v", err)
	}
}

func TestNotesClearByPath(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "", "notes", "add", "a.go", "-m", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := runNotes(t, "", "notes", "add", "sub/c.go", "-m", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := runNotes(t, "", "notes", "clear", "--path", "sub"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	out, _ := runNotes(t, "", "notes")
	if strings.Contains(out, "sub/c.go") || !strings.Contains(out, "a.go") {
		t.Fatalf("clear --path sub left the wrong notes:\n%s", out)
	}
}

// A multi-line body hangs under the line it continues, and --brief keeps its
// one-line promise.
func TestMultilineBodyLayout(t *testing.T) {
	notesRepo(t)
	if _, err := runNotes(t, "first\nsecond\n", "notes", "add", "a.go:2"); err != nil {
		t.Fatalf("add: %v", err)
	}

	out, err := runNotes(t, "", "notes")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	aligned := false
	for _, line := range strings.Split(out, "\n") {
		if line == "second" {
			t.Fatalf("the continuation is not indented:\n%s", out)
		}
		if strings.HasSuffix(line, "second") && strings.HasPrefix(line, " ") {
			aligned = true
		}
	}
	if !aligned {
		t.Fatalf("the continuation is not aligned:\n%s", out)
	}

	brief, err := runNotes(t, "", "notes", "--brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if strings.Contains(brief, "first\n") || !strings.Contains(brief, "first · second") {
		t.Fatalf("--brief is not one line per note:\n%s", brief)
	}
}
