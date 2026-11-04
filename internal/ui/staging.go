package ui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/ui/components/diffview"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// maxUndo bounds the reversal stack: a staging session is a few dozen decisions.
const maxUndo = 32

// syncStaging keeps the pane's cursor in step with what is on screen: staging is
// offered in the Files tab with the setting on, and everywhere else the pane is a
// plain reader.
func (m *model) syncStaging() {
	on := m.cfg.Stage &&
		m.state == stateShowList &&
		m.files.Tab() == filelist.TabFiles &&
		(m.allChanges || m.lastLoaded.path != "")
	m.view.SetSelectable(on)
	m.view.SetCursorFocus(m.focus == focusMain)
	m.view.SetSelectMode(m.cfg.StageApply == config.StageApplySelect)
	if !on {
		return
	}
	if m.allChanges && m.allLoading {
		// The document is still arriving: the end of the stream places the cursor.
		return
	}
	if m.anchorWait {
		// The operation's reload has not landed: restoring onto the document about to be
		// replaced would scroll to a change that is already gone.
		return
	}
	if m.stageAnchor == nil {
		// A freshly opened file has no cursor until the reader moves: place it.
		m.view.PlaceCursor()
		return
	}
	// Put the cursor back where the reader was, if the pane still shows that file. The
	// anchor is kept rather than spent: an operation triggers more than one reload.
	if m.lastLoaded.path != "" && m.stageAnchor.Path != m.lastLoaded.path {
		m.stageAnchor = nil
		return
	}
	m.view.RestoreCursor(*m.stageAnchor)
}

// stillChanged reports whether one side of a path is still a change, whatever the
// sidebar is filtered to: a file hidden by a filter has not been reverted.
func (m model) stillChanged(path string, staged bool) bool {
	for _, e := range m.files.AllEntries() {
		if e.Status.Path == path && e.Staged == staged {
			return true
		}
	}
	return false
}

// blockSides is the side of each block in a streamed chunk, one per listing row.
func blockSides(entries []filelist.Entry, from, n int) []diffview.BlockSide {
	out := make([]diffview.BlockSide, n)
	for i := range out {
		if from+i < len(entries) {
			out[i] = diffview.BlockSide{Staged: entries[from+i].Staged}
		}
	}
	return out
}

// dropAnchor forgets the reader's place, which any deliberate movement does.
func (m *model) dropAnchor() { m.stageAnchor, m.anchorWait = nil, false }

// notify sets a transient footer message.
func (m model) notify(s string) (tea.Model, tea.Cmd) {
	m.message = s
	return m, setMessageCmd(s)
}

// stageHere applies whatever the cursor names: the row decides whether that means
// the file, a hunk or one line, which is what keeps the interaction mode-free.
func (m model) stageHere() (tea.Model, tea.Cmd) {
	if !m.view.Selectable() {
		return m.notify("nothing to stage here")
	}
	if m.cfg.StageApply == config.StageApplySelect {
		// Marking a range spends it: the marks are the record of what was chosen.
		m.view.ToggleMark()
		m.view.ClearRange()
		return m, nil
	}
	path, staged, ok := m.cursorTarget()
	if !ok {
		return m.notify("nothing to stage here")
	}
	sel := m.view.Selection()
	if sel.Empty() {
		return m.notify("nothing under the cursor")
	}
	return m, m.stageCmd(stagingVerb(staged), path, sel, "", nil)
}

// cursorTarget names the file and side the cursor acts on: the one file on screen,
// or in the all-changes view the block the cursor is in, which says both.
func (m model) cursorTarget() (path string, staged bool, ok bool) {
	if path, ok := m.view.CursorFile(); ok {
		if m.allChanges {
			staged, ok := m.view.CursorStaged()
			return path, staged, ok
		}
		return path, m.lastLoaded.staged, true
	}
	if m.lastLoaded.path != "" && !m.allChanges {
		return m.lastLoaded.path, m.lastLoaded.staged, true
	}
	return "", false, false
}

// applyMarks sends the marked lines to the index in one operation.
func (m model) applyMarks() (tea.Model, tea.Cmd) {
	pending := m.view.Pending()
	if pending.Empty() {
		return m.notify("nothing marked")
	}
	path, staged, ok := m.cursorTarget()
	if !ok {
		return m.notify("nothing to apply here")
	}
	return m, m.stageCmd(stagingVerb(staged), path, pending, "", nil)
}

// stagingVerb is the direction the row's state implies: a staged line comes out, an
// unstaged one goes in.
func stagingVerb(staged bool) string {
	if staged {
		return "unstage"
	}
	return "stage"
}

// stageCmd runs one operation off the event loop and reports what it did, with the
// record that reverses it. An empty scope is filled from the pane; shown is the
// preview a discard promised, and git refuses the write if those lines moved.
func (m model) stageCmd(op, path string, sel *stage.Selection, scope string, shown []diff.Line) tea.Cmd {
	anchor := m.view.AnchorCursor(path)
	if scope == "" {
		scope = m.view.SelectionScope()
	}
	lines := sel.Len()
	return func() tea.Msg {
		var res git.StageResult
		var err error
		switch op {
		case "unstage":
			res, err = git.UnstageLines(path, sel)
		case "discard":
			res, err = git.DiscardLines(path, sel, shown)
		default:
			res, err = git.StageLines(path, sel)
		}
		return stageDoneMsg{
			staged: op == "stage",
			op:     op,
			path:   path,
			scope:  scope,
			lines:  lines,
			undo:   res.Undo,
			anchor: anchor,
			err:    err,
		}
	}
}

// discardHere throws away what the cursor names. It is the only destructive verb, so
// it is answered with a dialog showing the lines about to be lost.
func (m model) discardHere() (tea.Model, tea.Cmd) {
	if !m.view.Selectable() {
		return m.notify("nothing to discard here")
	}
	path, staged, ok := m.cursorTarget()
	if !ok {
		return m.notify("nothing to discard here")
	}
	if staged {
		return m.notify("discard works on unstaged changes")
	}
	sel := m.view.Selection()
	scope := m.view.SelectionScope()
	if m.view.SelectMode() && !m.view.Pending().Empty() {
		// In select mode the marks are the selection, so discard them.
		sel, scope = m.view.Pending(), "marks"
	}
	if sel.Empty() {
		return m.notify("nothing under the cursor")
	}
	if confirmRank(m.cfg.DiscardConfirm) <= 0 || scopeRank(scope) < confirmRank(m.cfg.DiscardConfirm) {
		return m, m.stageCmd("discard", path, sel, scope, nil)
	}
	digest := m.discardDigest(sel)
	m.state = stateConfirm
	m.confirm = &confirmState{
		path:  path,
		sel:   sel,
		lines: digest,
		scope: scope,
	}
	m.setSize()
	return m, nil
}

// confirmRank is how large a discard the dialog asks about, from nothing to a file.
func confirmRank(v string) int {
	switch v {
	case config.DiscardConfirmLine:
		return 1
	case config.DiscardConfirmHunk:
		return 2
	case config.DiscardConfirmFile:
		return 3
	}
	return 0
}

// scopeRank is how much a discard covers, on the same scale as confirmRank.
func scopeRank(scope string) int {
	switch scope {
	case "line":
		return 1
	case "hunk", "range", "marks":
		return 2
	case "file":
		return 3
	}
	return 0
}

// discardDigest lists the exact lines a discard would remove, so the dialog answers
// "which lines am I losing". Every line is kept, kind included: the dialog scrolls,
// and a preview that paints additions like deletions misreads the change.
func (m model) discardDigest(sel *stage.Selection) []diff.Line {
	const maxLines = 400
	var out []diff.Line
	if f := m.view.CursorDiffFile(); f != nil {
		for i := range f.Hunks {
			h, ok := f.HunkAt(i)
			if !ok {
				continue
			}
			for _, l := range h.Lines {
				key, ok := stage.LineOf(l)
				if !ok || !sel.Has(key) {
					continue
				}
				out = append(out, l)
				if len(out) == maxLines {
					more := fmt.Sprintf("… %d more", sel.Len()-maxLines)
					return append(out, diff.Line{Kind: diff.Context, Text: more})
				}
			}
		}
	}
	if len(out) == 0 {
		for i := 0; i < sel.Len() && i < maxLines; i++ {
			out = append(out, diff.Line{Kind: diff.Context, Text: "…"})
		}
	}
	return out
}

// confirmChrome is the dialog rows that are not preview: borders, path, blanks, hint.
const confirmChrome = 6

// confirmRows is how many preview lines fit, capped at a slice of the window so the
// pane behind stays visible.
func (m model) confirmRows() int {
	room := max(1, m.height-confirmChrome)
	return max(1, min(room, max(3, m.height*2/5)))
}

// confirmInnerWidth is the dialog's text column count, shared with the scroll
// arithmetic so the two cannot disagree.
func (m model) confirmInnerWidth() int { return max(20, min(66, m.width-4)) }

// confirmState is the discard dialog: the exact lines about to be lost, scrolling
// with the keys the diff pane uses.
type confirmState struct {
	path  string
	sel   *stage.Selection
	lines []diff.Line
	scope string
	// off is the first preview line on screen, x the cells scrolled sideways.
	off int
	x   int
}

// maxOffset is the last first-line that still fills the dialog.
func (c *confirmState) maxOffset(rows int) int { return max(0, len(c.lines)-rows) }

// maxX is how far the longest preview line can be scrolled sideways.
func (c *confirmState) maxX(innerW int) int {
	widest := 0
	for _, l := range c.lines {
		widest = max(widest, ansi.StringWidth(expandTabs(l.Text)))
	}
	return max(0, widest-innerW)
}

// expandTabs turns a tab into spaces before anything measures the line: a tab is one
// cell to the width arithmetic and eight to the terminal.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", "    ")
}

// updateConfirm answers the discard dialog. Confirming is enter, not y: `y` yanks in
// the diff pane, and enter is never the key that opened the dialog.
func (m model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cs := m.confirm
	if cs == nil {
		return m, nil
	}
	rows := m.confirmRows()
	maxX := cs.maxX(m.confirmInnerWidth())
	switch {
	// Enter, not Accept: that binding also takes l and the right arrow.
	case msg.String() == "enter":
		m.state = stateShowList
		m.confirm = nil
		m.setSize()
		return m, m.stageCmd("discard", cs.path, cs.sel, cs.scope, cs.lines)
	case msg.String() == "esc", msg.String() == "q", msg.String() == "n":
		m.state = stateShowList
		m.confirm = nil
		m.setSize()
		return m.notify("left it alone")
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case key.Matches(msg, m.keys.CursorUp):
		cs.off = max(0, cs.off-1)
	case key.Matches(msg, m.keys.CursorDown):
		cs.off = min(cs.maxOffset(rows), cs.off+1)
	case key.Matches(msg, m.keys.PageUp):
		cs.off = max(0, cs.off-rows)
	case key.Matches(msg, m.keys.PageDown):
		cs.off = min(cs.maxOffset(rows), cs.off+rows)
	case key.Matches(msg, m.keys.ScrollLeft):
		cs.x = max(0, cs.x-4)
	case key.Matches(msg, m.keys.ScrollRight):
		cs.x = min(maxX, cs.x+4)
	}
	return m, nil
}

// selectedSide is the file and side the listing has selected.
func (m model) selectedSide() (string, bool) {
	fs, staged := m.files.SelectedFile()
	if fs.Path == "" {
		return "", false
	}
	return fs.Path, staged
}

// undoLast reverses the most recent operation exactly, by writing the record
// that was there before it back.
func (m model) undoLast() (tea.Model, tea.Cmd) {
	if len(m.undo) == 0 {
		return m.notify("nothing to undo")
	}
	u := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	return m, undoCmd(u)
}

func undoCmd(u git.Undo) tea.Cmd {
	return func() tea.Msg {
		return undoDoneMsg{label: u.Label, err: u.Apply()}
	}
}

// applyStageDone folds a finished operation into the model: the reversal goes
// on the stack, the reader keeps their place, and the repository is re-read.
func (m model) applyStageDone(msg stageDoneMsg) (tea.Model, tea.Cmd) {
	if errors.Is(msg.err, git.ErrStale) {
		// The dialog's promise no longer holds. Nothing was written, so the
		// reader only has to look again.
		return m.notify("the file changed on disk, press x again")
	}
	if msg.err != nil {
		return m.notify(msg.err.Error())
	}
	if msg.undo.Label != "" {
		m.undo = append(m.undo, msg.undo)
		if len(m.undo) > maxUndo {
			m.undo = m.undo[1:]
		}
	}
	m.message = doneMessage(msg)

	// Both sides of the file changed, so neither cached diff is true any more.
	// Without this the immediate reload serves the pre-operation diff and
	// rebuilds the pane on content the reader just dealt with.
	for _, staged := range []bool{false, true} {
		m.cache.Delete(fileCacheKey(msg.path, staged, m.cfg))
	}

	fileScope := msg.scope == "file"
	if fileScope {
		// A whole file has just been dealt with, so the next thing to look at
		// is the next file. Staying on this one means staring at a file that is
		// no longer the job.
		m.advanceList = true
		m.stageAnchor = nil
	} else {
		anchor := msg.anchor
		m.stageAnchor = &anchor
		m.anchorWait = true
	}
	// The side the reader was working on: staging acts on the unstaged diff,
	// unstaging on the staged one, and discarding on the unstaged one. Keeping
	// it means the pane never switches the diff out from under them.
	sideStaged := msg.op == "unstage"
	// The cursor is dropped before the reload: the diff is about to change
	// shape under it, and a row index would then point at a line nobody chose.
	m.view.ClearCursor()

	if m.allChanges {
		// The pane holds the whole document, so reloading one file would
		// replace it with that file. Rebuild the document instead and let the
		// stream put the reader back where they were reading.
		m.allAnchor = m.view.Anchor()
		next, cmd := m.startAllChanges()
		nm := next.(model)
		nm.message = m.message
		return nm, tea.Batch(loadStatusCmd(false), cmd, setMessageCmd(nm.message))
	}
	if fileScope {
		return m, tea.Batch(loadStatusCmd(false), setMessageCmd(m.message))
	}

	next, load := m.showFile(msg.path, sideStaged)
	nm := next.(model)
	nm.message = m.message
	return nm, tea.Batch(loadStatusCmd(false), load, setMessageCmd(nm.message))
}

// doneMessage says what happened in the terms the reader used: how many lines,
// at what scope.
func doneMessage(msg stageDoneMsg) string {
	verb := msg.op
	if verb == "" {
		if msg.staged {
			verb = "stage"
		} else {
			verb = "unstage"
		}
	}
	past := "staged"
	switch verb {
	case "unstage":
		past = "unstaged"
	case "discard":
		past = "discarded"
	}
	// A whole file has no line count worth reporting; naming it says more.
	if msg.scope == "file" && msg.path != "" {
		return past + " " + msg.path
	}
	n := msg.lines
	unit := "line"
	if n != 1 {
		unit = "lines"
	}
	scope := msg.scope
	if scope == "" {
		scope = "selection"
	}
	return fmt.Sprintf("%s %d %s (%s)", past, n, unit, scope)
}

// nothingLeft says that a side of a file has no diff left, which is what
// staging or discarding its last line leaves behind. It is a plain statement
// rather than an error: the reader did the work, and the file is done.
func nothingLeft(path string, staged bool) string {
	if path == "" {
		return "nothing left to show"
	}
	if staged {
		return "nothing staged in " + path
	}
	return "nothing left in " + path
}
