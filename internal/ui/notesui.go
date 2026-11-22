package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/notes"
	"samouly.fun/twig/internal/ui/styles"
)

// newNoteInput is the note body bubble. alt+enter breaks a line, enter saves.
func newNoteInput() textarea.Model {
	in := textarea.New()
	in.Prompt = ""
	in.Placeholder = notePlaceholder
	in.CharLimit = 2048
	in.ShowLineNumbers = false
	in.DynamicHeight = true
	in.MinHeight = 1
	in.MaxHeight = 0
	km := textarea.DefaultKeyMap()
	km.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	in.KeyMap = km
	return in
}

// setNoteInputStyles paints the bubble in the tag's colours.
func (m *model) setNoteInputStyles() {
	_, _, pill, body := m.styles.Note(m.noteTag)
	state := textarea.StyleState{
		Base:             body,
		Text:             body,
		Placeholder:      body,
		Prompt:           body,
		CursorLine:       body,
		CursorLineNumber: body,
		LineNumber:       body,
		EndOfBuffer:      body,
		// Selecting hides the caret, so the highlight has to carry it: the
		// tag's colours inverted read on the wash and never disappear.
		Selection: lipgloss.NewStyle().
			Foreground(body.GetBackground()).
			Background(body.GetForeground()),
	}
	m.noteInput.SetStyles(textarea.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textarea.CursorStyle{Color: pill.GetBackground()},
	})
}

// notesLoadedMsg carries the store, its path and the work tree root.
type notesLoadedMsg struct {
	store notes.Store
	path  string
	root  string
	err   error
}

// loadNotesCmd reads the store once.
func loadNotesCmd() tea.Cmd {
	return func() tea.Msg {
		store, path, root, err := openNoteStore()
		return notesLoadedMsg{store: store, path: path, root: root, err: err}
	}
}

// openNoteStore loads the store of this repository.
func openNoteStore() (notes.Store, string, string, error) {
	gitDir, err := git.GitDir()
	if err != nil {
		return notes.Store{}, "", "", err
	}
	root, err := git.RepoRoot()
	if err != nil {
		return notes.Store{}, "", "", err
	}
	path := notes.Path(gitDir)
	store, err := notes.Load(path)
	return store, path, root, err
}

// withNotes adds the note rows to a file, wrapped to the pane's width. A paged
// file is left alone.
func withNotes(f *diff.File, list []notes.Note, width int) *diff.File {
	if f == nil || len(list) == 0 || len(f.Lines) == 0 {
		return f
	}
	// A file can arrive already carrying rows from an earlier pass (the
	// all-changes loader annotates, then the chunk annotates again): strip them
	// so a note is never drawn twice.
	f = withoutNotes(f)

	var head []notes.Note
	after := map[int][]notes.Note{}
	byHunk := map[int][]notes.Note{}
	for _, note := range list {
		switch note.Kind {
		case notes.File:
			head = append(head, note)
		case notes.Lines:
			row := lineRow(f, note)
			after[row] = append(after[row], note)
		case notes.Hunk:
			byHunk[note.Hunk] = append(byHunk[note.Hunk], note)
		}
	}
	if len(head) == 0 && len(after) == 0 && len(byHunk) == 0 {
		return f
	}

	out := *f
	out.Lines = make([]diff.Line, 0, len(f.Lines)+len(list))
	// A note whose line is gone hangs under the file notes.
	head = append(head, after[-1]...)
	for _, note := range head {
		out.Lines = append(out.Lines, noteRows(note, width)...)
	}
	hunk := 0
	var pending []notes.Note
	flush := func() {
		for _, note := range pending {
			out.Lines = append(out.Lines, noteRows(note, width)...)
		}
		pending = nil
	}
	for row, line := range f.Lines {
		if line.Kind == diff.HunkHeader {
			flush()
			hunk++
			pending = append(pending, byHunk[hunk]...)
			out.Lines = append(out.Lines, line)
			continue
		}
		out.Lines = append(out.Lines, line)
		for _, note := range after[row] {
			out.Lines = append(out.Lines, noteRows(note, width)...)
		}
	}
	flush()
	reindex(&out)
	return &out
}

// lineRow is the row a line note sits under: by text first, then by number.
func lineRow(f *diff.File, note notes.Note) int {
	want := ""
	if len(note.Text) > 0 {
		want = strings.TrimSuffix(note.Text[len(note.Text)-1], "\r")
	}
	byNumber := -1
	for row, line := range f.Lines {
		if line.Kind != diff.Context && line.Kind != diff.Addition {
			continue
		}
		if line.New != note.End {
			continue
		}
		byNumber = row
		if want == "" || line.Content() == want {
			return row
		}
	}
	if want != "" {
		for row, line := range f.Lines {
			if line.New == 0 || (line.Kind != diff.Context && line.Kind != diff.Addition) {
				continue
			}
			if line.Content() == want {
				return row
			}
		}
	}
	return byNumber
}

// withoutNotes strips the note rows.
func withoutNotes(f *diff.File) *diff.File {
	if f == nil || len(f.Lines) == 0 {
		return f
	}
	kept := make([]diff.Line, 0, len(f.Lines))
	for _, line := range f.Lines {
		if line.Kind != diff.Note {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(f.Lines) {
		return f
	}
	out := *f
	out.Lines = kept
	reindex(&out)
	return &out
}

// reindex points every hunk at its header row again.
func reindex(f *diff.File) {
	if len(f.Hunks) == 0 {
		return
	}
	f.Hunks = append([]diff.Hunk(nil), f.Hunks...)
	i := 0
	for row, line := range f.Lines {
		if line.Kind == diff.HunkHeader && i < len(f.Hunks) {
			f.Hunks[i].LineIndex = row
			i++
		}
	}
}

// noteRows is one note as rows. width 0 leaves the wrapping to the viewport.
func noteRows(note notes.Note, width int) []diff.Line {
	body := strings.Split(strings.TrimRight(note.Body, "\n"), "\n")
	rows := make([]diff.Line, 0, len(body))
	pill := notePillWidth(note.Tag)
	for _, line := range body {
		for _, seg := range noteSegments(line, width, pill) {
			rows = append(rows, diff.Line{
				Kind:     diff.Note,
				NoteID:   note.ID,
				NoteTag:  note.Tag,
				NoteTail: len(rows) > 0,
				Text:     seg,
				New:      note.End,
			})
		}
	}
	return rows
}

// noteStyles measures the tag pills.
var noteStyles = styles.New()

// notePillWidth is the cells a tag's pill takes.
func notePillWidth(tag notes.Tag) int {
	icon, word, _, _ := noteStyles.Note(tag)
	return lipgloss.Width(" " + icon + " " + word + " ")
}

// noteSegments wraps one body line to the row's cells.
func noteSegments(line string, width, pill int) []string {
	if width <= 0 {
		return []string{line}
	}
	w := max(16, width-pill-1)
	return strings.Split(ansi.Hardwrap(ansi.Wordwrap(line, w, ""), w, true), "\n")
}

// annotateFiles adds each file's notes to it, before an aggregate is built.
func (m model) annotateFiles(files []*diff.File) []*diff.File {
	if len(m.noteStore.Notes) == 0 {
		return files
	}
	out := make([]*diff.File, len(files))
	for i, file := range files {
		if file == nil {
			out[i] = nil
			continue
		}
		out[i] = withNotes(file, m.noteStore.For(file.Path()), m.noteWidth())
	}
	return out
}

// notesAt returns the notes anchored to a line of one path.
func notesAt(store notes.Store, path string, line int) []notes.Note {
	var out []notes.Note
	for _, note := range store.For(path) {
		if note.Kind == notes.Lines && note.Start <= line && line <= note.End {
			out = append(out, note)
		}
	}
	return out
}

// cursorLine is the file and line under the pane's cursor.
func (m model) cursorLine() (string, int, bool) {
	file := m.view.File()
	if file == nil {
		return "", 0, false
	}
	line, ok := m.view.RowAnchor(m.view.CursorRow())
	if !ok || line.New == 0 {
		return "", 0, false
	}
	path := m.lastLoaded.path
	if path == "" {
		// All changes: the document holds several files, so ask the row.
		if p, ok := m.view.CursorFile(); ok {
			path = p
		}
	}
	if path == "" {
		path = file.Path()
	}
	if path == "" {
		return "", 0, false
	}
	return path, line.New, true
}

// reannotate puts the notes back, keeping the reader on their line.
func (m *model) reannotate() {
	file := m.view.File()
	if file == nil || m.lastLoaded.path == "" {
		return
	}
	line := 0
	onNote := ""
	if l, ok := m.view.RowAnchor(m.view.CursorRow()); ok {
		line = l.New
		if l.Kind == diff.Note {
			onNote = l.NoteID
		}
	}
	selectable := m.view.Selectable()
	off := m.view.YOffset()
	m.view.SetNoteDrift(m.noteDrifted)

	plain := withoutNotes(file)
	annotated := withNotes(plain, m.noteStore.For(m.lastLoaded.path), m.noteWidth())
	m.view.SetFile(annotated)
	m.view.SetSelectable(selectable)

	// SetFile starts at the top: put the reader back where they were reading,
	// after the cursor has been placed, so the cursor never drags the pane.
	defer m.view.SetScroll(off)

	// A reload must not walk the cursor off the note it was on: X would then
	// drop every note sharing that line.
	if onNote != "" {
		for row := 0; row < m.view.Rows(); row++ {
			if l, ok := m.view.RowLine(row); ok && l.Kind == diff.Note && l.NoteID == onNote {
				m.view.CursorTo(row)
				return
			}
		}
	}
	if line == 0 {
		return
	}
	if row := indexOfLine(annotated, line); row >= 0 {
		m.view.CursorTo(row + m.view.PreRows())
	}
}

// noteDrifted reports whether a note's anchor moved, so its row can say so.
func (m model) noteDrifted(id string) bool {
	for _, note := range m.noteStore.Notes {
		if note.ID == id {
			return note.Drifted(m.notesRoot)
		}
	}
	return false
}

// noteWidth is 0 when the viewport soft-wraps rows itself.
func (m model) noteWidth() int { return m.view.NoteWidth() }

func (m model) noteDraftWidth() int {
	w := m.noteWidth()
	if w <= 0 {
		w = m.mainRect.Dx() - 2*m.paneInset()
	}
	return max(20, w-notePillWidth(m.noteTag)-1)
}

// indexOfLine is the file row that paints a new-side line.
func indexOfLine(f *diff.File, line int) int {
	for i := len(f.Lines) - 1; i >= 0; i-- {
		if f.Lines[i].New == line && f.Lines[i].Kind != diff.Note {
			return i
		}
	}
	return -1
}

// cursorToLine puts the cursor on a new-side line.
func (m *model) cursorToLine(line int) {
	file := m.view.File()
	if file == nil {
		return
	}
	if row := indexOfLine(file, line); row >= 0 {
		m.view.CursorTo(row + m.view.PreRows())
	}
}

// startNote opens a typing row at the cursor's line.
func (m model) startNote() (tea.Model, tea.Cmd) {
	path, line, ok := m.cursorLine()
	if !ok {
		return m.notify("no line under the cursor")
	}
	file := m.view.File()
	anchor := indexOfLine(file, line)
	if anchor < 0 {
		return m.notify("no line under the cursor")
	}
	row := anchor + 1
	file.Lines = append(file.Lines, diff.Line{})
	copy(file.Lines[row+1:], file.Lines[row:])
	file.Lines[row] = diff.Line{Kind: diff.Note, NoteTag: notes.Fyi, Text: notePlaceholder, New: line}
	reindex(file)
	m.view.Refresh()
	m.view.CursorTo(row + m.view.PreRows())

	m.noteMode, m.notePath, m.noteLine, m.noteRow = true, path, line, row
	m.noteDraftRows = 1
	m.noteTag = notes.Fyi
	m.noteInput.SetValue("")
	// Focus first: an unfocused bubble paints with empty styles.
	cmd := m.noteInput.Focus()
	m.paintDraft()
	return m, cmd
}

// notePlaceholder is what an empty typing row says.
const notePlaceholder = "type a note · alt+enter: new line · tab: tag · enter: save"

// updateNoteKey edits the typing row.
func (m model) updateNoteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.noteMode = false
		m.noteInput.Blur()
		m.removeDraft()
		return m, nil
	case "enter":
		return m.saveNote()
	case "tab":
		// Tab switches the pill and its colours; it never touches the text.
		m.noteTag = m.noteTag.Next()
		m.paintDraft()
		return m, nil
	}

	updated, cmd := m.noteInput.Update(msg)
	m.noteInput = updated
	m.paintDraft()
	return m, cmd
}

// paintDraft rewrites the draft rows from the bubble.
func (m *model) paintDraft() {
	file := m.view.File()
	if file == nil || m.noteRow < 0 || m.noteRow > len(file.Lines) {
		return
	}
	tag, _ := m.noteTagNow()
	m.noteTag = tag
	m.setNoteInputStyles()
	m.noteInput.SetWidth(m.noteDraftWidth())
	m.replaceDraft(file, m.draftRows(tag))
	m.view.Refresh()
	m.view.CursorTo(m.noteRow + m.view.PreRows())
}

func (m model) draftRows(tag notes.Tag) []diff.Line {
	lines := strings.Split(strings.TrimRight(m.noteInput.View(), "\n"), "\n")
	rows := make([]diff.Line, 0, len(lines))
	for i, line := range lines {
		rows = append(rows, diff.Line{
			Kind:     diff.Note,
			NoteTag:  tag,
			NoteTail: i > 0,
			Text:     line,
			New:      m.noteLine,
		})
	}
	return rows
}

func (m *model) replaceDraft(file *diff.File, rows []diff.Line) {
	end := min(len(file.Lines), m.noteRow+max(1, m.noteDraftRows))
	out := make([]diff.Line, 0, len(file.Lines)-(end-m.noteRow)+len(rows))
	out = append(out, file.Lines[:m.noteRow]...)
	out = append(out, rows...)
	out = append(out, file.Lines[end:]...)
	file.Lines = out
	m.noteDraftRows = len(rows)
	reindex(file)
}

// noteTagNow is the tag and body of the draft: a leading tag word wins, and
// otherwise the tag tab picked does.
func (m model) noteTagNow() (notes.Tag, string) {
	value := strings.TrimSpace(m.noteInput.Value())
	if tag, body := notes.SplitTag(value); body != value {
		return tag, body
	}
	return m.noteTag, value
}

// removeDraft drops the typing row without rebuilding the document.
func (m *model) removeDraft() {
	file := m.view.File()
	if file == nil || m.noteRow < 0 || m.noteRow >= len(file.Lines) {
		return
	}
	end := min(len(file.Lines), m.noteRow+max(1, m.noteDraftRows))
	file.Lines = append(file.Lines[:m.noteRow], file.Lines[end:]...)
	m.noteDraftRows = 0
	reindex(file)
	m.view.Refresh()
	m.cursorToLine(m.noteLine)
}

// finishDraft turns the typing rows into the saved note.
func (m *model) finishDraft(id string, tag notes.Tag, body string) {
	file := m.view.File()
	if file == nil || m.noteRow < 0 || m.noteRow > len(file.Lines) {
		return
	}
	note := notes.Note{ID: id, Kind: notes.Lines, Tag: tag, Path: m.notePath, Start: m.noteLine, End: m.noteLine, Body: body}
	m.replaceDraft(file, noteRows(note, m.noteWidth()))
	m.noteDraftRows = 0
	m.view.Refresh()
	m.view.CursorTo(m.noteRow + m.view.PreRows())
}

// saveNote writes the note the typing row holds.
func (m model) saveNote() (tea.Model, tea.Cmd) {
	m.noteMode = false
	m.noteInput.Blur()
	tag, text := m.noteTagNow()
	if text == "" {
		m.removeDraft()
		return m.notify("a note needs a body")
	}

	store, path, root, err := openNoteStore()
	if err != nil {
		m.removeDraft()
		return m.notify(err.Error())
	}
	note := notes.Note{
		Kind:    notes.Lines,
		Tag:     tag,
		Path:    m.notePath,
		Start:   m.noteLine,
		End:     m.noteLine,
		Body:    text,
		Created: time.Now().UTC(),
	}
	if err := note.Resolve(root); err != nil {
		m.removeDraft()
		return m.notify(err.Error())
	}
	added := store.Add(note)
	if err := store.Save(path); err != nil {
		m.removeDraft()
		return m.notify(err.Error())
	}
	m.noteStore, m.notesPath, m.notesRoot = store, path, root
	m.view.SetNoteDrift(m.noteDrifted)
	m.finishDraft(added.ID, tag, text)
	return m.notify("note " + added.ID + " added")
}

// dropNote removes the note under the cursor, or those on its line.
func (m model) dropNote() (tea.Model, tea.Cmd) {
	row, ok := m.view.RowAnchor(m.view.CursorRow())
	if !ok {
		return m.notify("no line under the cursor")
	}

	var ids []string
	if row.Kind == diff.Note && row.NoteID != "" {
		ids = []string{row.NoteID}
	} else {
		path, lineNo, ok := m.cursorLine()
		if !ok {
			return m.notify("no note on this line")
		}
		for _, note := range notesAt(m.noteStore, path, lineNo) {
			ids = append(ids, note.ID)
		}
	}
	if len(ids) == 0 {
		return m.notify("no note on this line")
	}

	store, storePath, _, err := openNoteStore()
	if err != nil {
		return m.notify(err.Error())
	}
	store.Remove(ids...)
	if err := store.Save(storePath); err != nil {
		return m.notify(err.Error())
	}
	m.noteStore = store
	m.reannotate()
	return m.notify("note removed")
}
