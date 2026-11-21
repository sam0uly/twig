package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/notes"
	"samouly.fun/twig/internal/ui/components/diffview"
)

const twoHunks = "diff --git a/a.go b/a.go\n" +
	"--- a/a.go\n" +
	"+++ b/a.go\n" +
	"@@ -1,3 +1,3 @@\n" +
	" one\n-two\n+TWO\n three\n" +
	"@@ -20,3 +20,3 @@\n" +
	" twenty\n-old\n+new\n tail\n"

func parsedFile(t *testing.T) *diff.File {
	t.Helper()
	d := diff.Parse(twoHunks)
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}
	return d.Files[0]
}

func TestNoteRowSitsUnderItsLine(t *testing.T) {
	file := parsedFile(t)
	list := []notes.Note{{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "look here"}}

	got := withNotes(file, list, 0)
	var rows []string
	for _, line := range got.Lines {
		rows = append(rows, line.Text)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "+TWO\nlook here") {
		t.Fatalf("the note is not under its line:\n%s", joined)
	}
	// Hunk bodies stay clean.
	for _, hunk := range got.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == diff.Note {
				t.Fatal("a note leaked into a hunk body")
			}
		}
	}
}

func TestHunkNoteSitsAfterItsHunk(t *testing.T) {
	file := parsedFile(t)
	list := []notes.Note{{Kind: notes.Hunk, Path: "a.go", Hunk: 2, Body: "second hunk"}}

	got := withNotes(file, list, 0)
	var rows []string
	for _, line := range got.Lines {
		rows = append(rows, line.Text)
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, " tail\nsecond hunk") {
		t.Fatalf("the hunk note is not after its hunk:\n%s", joined)
	}
	if strings.Contains(joined, " three\nsecond hunk") {
		t.Fatalf("the hunk note landed in the first hunk:\n%s", joined)
	}
}

func TestHunkIndicesFollowTheInsertedRows(t *testing.T) {
	file := parsedFile(t)
	list := []notes.Note{
		{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "one"},
		{Kind: notes.Lines, Path: "a.go", Start: 21, End: 21, Body: "two"},
	}

	got := withNotes(file, list, 0)
	for i, hunk := range got.Hunks {
		if hunk.LineIndex >= len(got.Lines) || got.Lines[hunk.LineIndex].Kind != diff.HunkHeader {
			t.Fatalf("hunk %d points at row %d, not a header", i, hunk.LineIndex)
		}
		if got.Lines[hunk.LineIndex].Text != file.Hunks[i].Header {
			t.Fatalf("hunk %d points at %q, want %q", i, got.Lines[hunk.LineIndex].Text, file.Hunks[i].Header)
		}
	}
}

func TestWithoutNotesRoundTrips(t *testing.T) {
	file := parsedFile(t)
	list := []notes.Note{{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "look"}}

	annotated := withNotes(file, list, 0)
	plain := withoutNotes(annotated)
	if len(plain.Lines) != len(file.Lines) {
		t.Fatalf("stripped file has %d rows, want %d", len(plain.Lines), len(file.Lines))
	}
	for i, line := range plain.Lines {
		if line.Text != file.Lines[i].Text {
			t.Fatalf("row %d = %q, want %q", i, line.Text, file.Lines[i].Text)
		}
	}
	// Annotating twice must not stack notes.
	again := withNotes(plain, list, 0)
	count := 0
	for _, line := range again.Lines {
		if line.Kind == diff.Note {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d note rows after re-annotating, want 1", count)
	}
}

func TestNotesAt(t *testing.T) {
	store := notes.Store{NextID: 1}
	store.Add(notes.Note{Kind: notes.Lines, Path: "a.go", Start: 2, End: 4, Body: "range"})
	store.Add(notes.Note{Kind: notes.Lines, Path: "a.go", Start: 9, End: 9, Body: "line"})
	store.Add(notes.Note{Kind: notes.File, Path: "a.go", Body: "file"})

	if got := notesAt(store, "a.go", 3); len(got) != 1 || got[0].Body != "range" {
		t.Fatalf("notesAt(3) = %+v", got)
	}
	if got := notesAt(store, "a.go", 9); len(got) != 1 || got[0].Body != "line" {
		t.Fatalf("notesAt(9) = %+v", got)
	}
	if got := notesAt(store, "a.go", 5); len(got) != 0 {
		t.Fatalf("notesAt(5) = %+v", got)
	}
}

// noteRepo is a repository with one changed file.
func noteRepo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "."},
		{"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

// M writes a note, the row appears, X takes it away.
func TestNoteAddedAndDroppedFromThePane(t *testing.T) {
	noteRepo(t)

	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	if !m.noteMode {
		t.Fatal("M did not open the prompt")
	}
	if row := m.view.CursorRow(); row != m.noteRow+m.view.PreRows() {
		t.Fatalf("the cursor is at %d, not on the typing row %d", row, m.noteRow)
	}
	m.noteInput.SetValue("from the pane")
	m.paintDraft()

	next, _ = m.saveNote()
	m = next.(model)
	if m.noteMode {
		t.Fatal("the prompt stayed open after enter")
	}
	if len(m.noteStore.Notes) != 1 {
		t.Fatalf("store holds %d notes, want 1", len(m.noteStore.Notes))
	}
	note := m.noteStore.Notes[0]
	if note.Kind != notes.Lines || note.Start != 2 || note.End != 2 || note.Path != "a.go" {
		t.Fatalf("note = %+v, want a line note on a.go:2", note)
	}
	if !paneHasNote(m.view.File(), "from the pane") {
		t.Fatal("the note row is not in the pane")
	}

	next, _ = m.dropNote()
	m = next.(model)
	if len(m.noteStore.Notes) != 0 {
		t.Fatalf("store holds %d notes after X, want 0", len(m.noteStore.Notes))
	}
	if paneHasNote(m.view.File(), "from the pane") {
		t.Fatal("the note row survived X")
	}
}

func mustNoteStore(t *testing.T) (notes.Store, string, string) {
	t.Helper()
	store, path, root, err := openNoteStore()
	if err != nil {
		t.Fatalf("openNoteStore: %v", err)
	}
	return store, path, root
}

// selectLine puts the cursor on line 2 of the fixture and answers its row.
func selectLine(t *testing.T, m model) int {
	t.Helper()
	file := m.view.File()
	for i, l := range file.Lines {
		if l.New == 2 && l.Kind != diff.Note {
			row := i + m.view.PreRows()
			m.view.CursorTo(row)
			return row
		}
	}
	t.Fatal("no row for new line 2")
	return 0
}

func paneHasNote(file *diff.File, body string) bool {
	for _, line := range file.Lines {
		if line.Kind == diff.Note && strings.Contains(line.Text, body) {
			return true
		}
	}
	return false
}

// A note written against an older diff keeps its line through its text.
func TestLineNoteFollowsItsText(t *testing.T) {
	note := notes.Note{Kind: notes.Lines, Path: "a.go", Start: 1, End: 1, Text: []string{"TWO"}, Body: "moved"}
	got := withNotes(parsedFile(t), []notes.Note{note}, 0)

	row := noteRowIndex(got)
	if row < 1 || got.Lines[row-1].Text != "+TWO" {
		t.Fatalf("the note did not land under the line it quotes:\n%s", lineDump(got))
	}
}

// The note row carries the anchor's number, so the gutter reads it as that line.
func TestNoteRowShowsTheAnchoredLineNumber(t *testing.T) {
	got := withNotes(parsedFile(t), []notes.Note{
		{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "x"},
	}, 0)
	row := noteRowIndex(got)
	if row < 0 {
		t.Fatal("no note row")
	}
	if got.Lines[row].New != 2 {
		t.Fatalf("note row number = %d, want 2", got.Lines[row].New)
	}
}

func TestLineNoteFallsBackToItsNumber(t *testing.T) {
	note := notes.Note{Kind: notes.Lines, Path: "a.go", Start: 3, End: 3, Text: []string{"gone"}, Body: "by number"}
	got := withNotes(parsedFile(t), []notes.Note{note}, 0)

	row := noteRowIndex(got)
	if row < 1 || got.Lines[row-1].Text != " three" {
		t.Fatalf("the note did not land under line 3:\n%s", lineDump(got))
	}
}

func noteRowIndex(f *diff.File) int {
	for i, l := range f.Lines {
		if l.Kind == diff.Note {
			return i
		}
	}
	return -1
}

func lineDump(f *diff.File) string {
	var b strings.Builder
	for i, l := range f.Lines {
		fmt.Fprintf(&b, "%2d kind=%d old=%d new=%d %q\n", i, l.Kind, l.Old, l.New, l.Text)
	}
	return b.String()
}

func TestFileNoteSitsAboveTheFirstHunk(t *testing.T) {
	file := parsedFile(t)
	list := []notes.Note{{Kind: notes.File, Path: "a.go", Body: "read me first"}}

	got := withNotes(file, list, 0)
	if got.Lines[0].Kind != diff.Note || got.Lines[0].Text != "read me first" {
		t.Fatalf("first row = %+v, want the file note", got.Lines[0])
	}
	if got.Lines[1].Kind != diff.HunkHeader {
		t.Fatalf("second row = %+v, want the first hunk header", got.Lines[1])
	}
}

func TestTypingRowIsSelectableAndShowsAHint(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	if m.view.CursorItem().Kind != diffview.ItemLine {
		t.Fatal("the typing row is not selectable")
	}
	view := m.view.View()
	if !strings.Contains(view, "tab: tag · enter: save") {
		t.Fatalf("the typing row shows no hint:\n%s", view)
	}

	m.noteInput.SetValue("saved body")
	m.paintDraft()
	next, _ = m.saveNote()
	m = next.(model)
	if strings.Contains(m.view.View(), notePlaceholder) {
		t.Fatal("the placeholder survived saving")
	}
	if !strings.Contains(m.view.View(), "saved body") {
		t.Fatalf("the saved body is not rendered:\n%s", m.view.View())
	}
}

func TestXOnTheNoteRowDropsIt(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	m.noteInput.SetValue("drop me")
	m.paintDraft()
	next, _ = m.saveNote()
	m = next.(model)

	row := -1
	for i, line := range m.view.File().Lines {
		if line.Kind == diff.Note {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("no note row to sit on")
	}
	m.view.CursorTo(row)
	next, _ = m.dropNote()
	m = next.(model)
	if len(m.noteStore.Notes) != 0 {
		t.Fatalf("X on the note row left %d notes", len(m.noteStore.Notes))
	}
}

func TestNoteTagPill(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)

	// The pill follows what is typed.
	m.noteInput.SetValue("todo: check this")
	m.paintDraft()
	view := m.view.View()
	if !strings.Contains(view, "TODO") {
		t.Fatalf("the typing row has no TODO pill:\n%s", view)
	}
	if !strings.Contains(view, "todo: check this") {
		t.Fatalf("the draft is not shown as typed:\n%s", view)
	}
	// The input uses the tag's colours.
	if !strings.Contains(view, "104;255;214") || !strings.Contains(view, "28;54;52") {
		t.Fatalf("the input is not painted in the tag's colours:\n%s", view)
	}

	next, _ = m.saveNote()
	m = next.(model)
	if len(m.noteStore.Notes) != 1 {
		t.Fatalf("saved notes = %+v", m.noteStore.Notes)
	}
	note := m.noteStore.Notes[0]
	if note.Tag != notes.Todo || note.Body != "check this" {
		t.Fatalf("saved note = %+v, want a todo with body %q", note, "check this")
	}
	if !strings.Contains(m.view.View(), "TODO") {
		t.Fatalf("the saved row lost its pill:\n%s", m.view.View())
	}
}

// Typing holds the follow, or the input row would disappear mid-word.
// Tab switches the pill and nothing else: the text is left as typed.
func TestTabOnlySwitchesThePill(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	m.noteInput.SetValue("please fix")
	m.paintDraft()

	next, _ = m.updateNoteKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(model)
	if got := m.noteInput.Value(); got != "please fix" {
		t.Fatalf("tab rewrote the text: %q", got)
	}
	if !strings.Contains(m.view.View(), "TODO") {
		t.Fatalf("tab did not switch the pill:\n%s", m.view.View())
	}

	next, _ = m.saveNote()
	m = next.(model)
	if len(m.noteStore.Notes) != 1 {
		t.Fatalf("saved notes = %+v", m.noteStore.Notes)
	}
	if note := m.noteStore.Notes[0]; note.Tag != notes.Todo || note.Body != "please fix" {
		t.Fatalf("saved note = %+v", note)
	}
}

func TestTypingHoldsTheLiveFollow(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.cfg.Follow, m.cfg.Watch = true, true
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	if !m.noteMode {
		t.Fatal("M did not open the input")
	}
	if m.followShown("a.go", false, parsedFile(t)) {
		t.Fatal("the follow moved the pane while a note was being typed")
	}
	if _, cmd := m.autoLoadFile(); cmd != nil {
		t.Fatal("a reload was queued while a note was being typed")
	}
}

// esc leaves the input without a rebuild or a jump.
func TestEscLeavesTheInputInPlace(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	row := selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	m.noteInput.SetValue("half typed")
	m.paintDraft()

	next, _ = m.updateNoteKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if m.noteMode {
		t.Fatal("esc left the input open")
	}
	for _, line := range m.view.File().Lines {
		if line.Kind == diff.Note {
			t.Fatalf("the typing row survived esc:\n%s", lineDump(m.view.File()))
		}
	}
	if got := m.view.CursorRow(); got != row {
		t.Fatalf("esc moved the cursor to row %d, want %d", got, row)
	}
}

func TestNoteRowsSurviveWrap(t *testing.T) {
	noteRepo(t)
	m := testModel(60, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetWrap(true)
	m.view.SetFile(withNotes(parsedFile(t), []notes.Note{
		{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "a long note body that has to wrap over several display lines when the pane is narrow"},
	}, 0))
	m.view.SetSelectable(true)

	view := m.view.View()
	if !strings.Contains(view, "display lines when the pane") {
		t.Fatalf("the wrapped note body is missing:\n%s", view)
	}
}

// A long note wraps on its own, so it is readable with soft wrap off.
func TestNoteBodyWrapsWithoutDiffWrap(t *testing.T) {
	note := notes.Note{
		Kind: notes.Lines, Path: "a.go", Start: 2, End: 2,
		Body: "a long note body that has to wrap over several rows even though the diff does not soft wrap it",
	}

	got := withNotes(parsedFile(t), []notes.Note{note}, 40)
	var rows []diff.Line
	for _, line := range got.Lines {
		if line.Kind == diff.Note {
			rows = append(rows, line)
		}
	}
	if len(rows) < 2 {
		t.Fatalf("the note did not wrap: %d rows", len(rows))
	}
	if rows[0].NoteTail {
		t.Fatal("the first row is marked as a continuation")
	}
	for _, row := range rows[1:] {
		if !row.NoteTail {
			t.Fatalf("a continuation row is not marked: %q", row.Text)
		}
	}
	for _, row := range rows {
		if len(row.Text) > 40 {
			t.Fatalf("a row is %d cells wide in a 40 cell pane: %q", len(row.Text), row.Text)
		}
	}
}

// The pill belongs to the note, not to each of its rows.
func TestOnlyTheFirstRowPaintsThePill(t *testing.T) {
	noteRepo(t)
	m := testModel(80, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	body := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen"
	m.view.SetFile(withNotes(parsedFile(t), []notes.Note{
		{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: body},
	}, 40))

	if n := strings.Count(m.view.View(), "FYI"); n != 1 {
		t.Fatalf("the pill is painted %d times, want 1:\n%s", n, m.view.View())
	}
}

// Enter saves, alt+enter breaks the line, and the block grows with it.
func TestDraftGrowsWithALineBreak(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	if m.noteDraftRows != 1 {
		t.Fatalf("a fresh draft takes %d rows, want 1", m.noteDraftRows)
	}
	m.noteInput.SetValue("first line\nsecond line")
	m.paintDraft()
	if m.noteDraftRows != 2 {
		t.Fatalf("the draft takes %d rows, want 2", m.noteDraftRows)
	}
	tails := 0
	for _, line := range m.view.File().Lines {
		if line.Kind == diff.Note && line.NoteTail {
			tails++
		}
	}
	if tails != 1 {
		t.Fatalf("tail rows = %d, want 1", tails)
	}

	next, _ = m.saveNote()
	m = next.(model)
	if got := m.noteStore.Notes[0].Body; got != "first line\nsecond line" {
		t.Fatalf("saved body = %q", got)
	}
	if m.noteDraftRows != 0 {
		t.Fatalf("the draft still claims %d rows", m.noteDraftRows)
	}
}

// A note written by the shell shows up without pressing r.
func TestNotesReloadWhenTheStoreChanges(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.notesStamp = fileStamp(m.notesPath)

	store, path := m.noteStore, m.notesPath
	store.Add(notes.Note{Kind: notes.Lines, Path: "a.go", Start: 2, End: 2, Body: "from the shell"})
	if err := store.Save(path); err != nil {
		t.Fatal(err)
	}

	next, cmd := m.watchNotes()
	m = next.(model)
	if cmd == nil {
		t.Fatal("a changed store scheduled no reload")
	}
	m = runCmd(m, cmd, 0)
	if len(m.noteStore.Notes) != 1 || m.noteStore.Notes[0].Body != "from the shell" {
		t.Fatalf("the store did not reload: %+v", m.noteStore.Notes)
	}
}

// alt+enter breaks the line through the real key path; enter still saves.
func TestAltEnterBreaksTheLine(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)
	m.noteInput.SetValue("first")
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	m = next.(model)

	if !m.noteMode {
		t.Fatal("alt+enter saved the note instead of breaking the line")
	}
	if !strings.Contains(m.noteInput.Value(), "\n") {
		t.Fatalf("alt+enter left the value as %q", m.noteInput.Value())
	}
	if m.noteDraftRows != 2 {
		t.Fatalf("the draft takes %d rows after a line break, want 2", m.noteDraftRows)
	}
}

// Selecting hides the caret, so the highlight has to be painted.
func TestDraftSelectionHasAColour(t *testing.T) {
	noteRepo(t)
	m := testModel(120, 30)
	m.noteStore, m.notesPath, m.notesRoot = mustNoteStore(t)
	m.lastLoaded.path = "a.go"
	m.view.SetFile(parsedFile(t))
	m.view.SetSelectable(true)
	selectLine(t, m)

	next, _ := m.startNote()
	m = next.(model)

	sel := m.noteInput.Styles().Focused.Selection
	if _, none := sel.GetBackground().(lipgloss.NoColor); none {
		t.Fatal("the selection has no background")
	}

	// And it reaches the screen: the FYI selection is Oceania on Lichen, a
	// combination nothing else in the note row paints.
	m.noteInput.SetValue("pick these words")
	m.paintDraft()
	m.noteInput.SelectAll()
	m.paintDraft()
	if !strings.Contains(m.view.View(), "38;2;43;85;179;48;2;92;223;234") {
		t.Fatalf("the selection is not painted:\n%s", m.view.View())
	}
}
