package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// keyPress builds the message a terminal sends for one keystroke.
func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// TestSpaceOnAChangeLineStagesIt checks the key routing: the pane has a file,
// the cursor sits on a change line, and space produces a line-scoped operation
// rather than a message.
func TestSpaceOnAChangeLineStagesIt(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.lastLoaded.path = "a.go"
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"one\ntwo\nthree\n", "one\nTWO\nthree\n", diff.GenerateOptions{Context: 3}))
	m.syncStaging()
	if !m.view.Selectable() {
		t.Fatal("the pane is not offering staging")
	}
	if !m.view.MoveCursor(1) {
		t.Fatal("the cursor did not move")
	}
	for i := 0; i < 8 && m.view.SelectionScope() != "line"; i++ {
		if !m.view.MoveCursor(1) {
			t.Fatal("the cursor stopped before reaching a change line")
		}
	}
	if got := m.view.SelectionScope(); got != "line" {
		t.Fatalf("scope %q, want line", got)
	}

	next, cmd := m.stageHere()
	if cmd == nil {
		t.Fatalf("space produced no operation: %q", next.(model).message)
	}
	// Running it outside a repository must still report a well-formed
	// operation; the git call itself fails, which is the correct outcome.
	msg, ok := cmd().(stageDoneMsg)
	if !ok {
		t.Fatal("the command did not produce a stageDoneMsg")
	}
	if msg.op != "stage" || msg.scope != "line" || msg.lines != 2 {
		t.Fatalf("operation was %q/%q/%d, want the replacement pair", msg.op, msg.scope, msg.lines)
	}
	if msg.err == nil {
		t.Fatal("a staging operation outside a repository should have failed")
	}
}

// TestSyncStagingGatesOnContext makes sure the cursor is offered exactly where
// staging exists, and nowhere else: not for commits, not for the aggregate
// view, not when the setting is off.
func TestSyncStagingGatesOnContext(t *testing.T) {
	cases := []struct {
		name string
		mut  func(m model) model
		want bool
	}{
		{"files tab", func(m model) model { return m }, true},
		{"setting off", func(m model) model { m.cfg.Stage = false; return m }, false},
		{"commits tab", func(m model) model { m.files.SetTab(filelist.TabCommits); return m }, false},
		// Staging works in the all-changes document, where the cursor names
		// the file its block belongs to.
		{"all changes", func(m model) model {
			m.allChanges = true
			m.lastLoaded.path = ""
			return m
		}, true},
		{"no file", func(m model) model { m.lastLoaded.path = ""; return m }, false},
		{"dialog open", func(m model) model { m.state = stateConfirm; return m }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The panes are behind pointers, so each case needs its own model:
			// a mutation from one case would otherwise leak into the next.
			m := testModel(120, 24)
			m.lastLoaded.path = "a.go"
			m = c.mut(m)
			m.syncStaging()
			if got := m.view.Selectable(); got != c.want {
				t.Fatalf("selectable = %v, want %v", got, c.want)
			}
		})
	}
}

// TestStageDoneRecordsUndo keeps the promise that every operation is reversible
// by the same keystroke that made it.
func TestStageDoneRecordsUndo(t *testing.T) {
	m := testModel(120, 24)
	next, cmd := m.applyStageDone(stageDoneMsg{
		staged: true,
		op:     "stage",
		path:   "a.go",
		scope:  "line",
		lines:  1,
		undo:   git.Undo{Path: "a.go", Mode: "100644", OID: "abc", HadIndex: true, Label: "stage"},
	})
	if cmd == nil {
		t.Fatal("no refresh was scheduled")
	}
	got := next.(model)
	if len(got.undo) != 1 {
		t.Fatalf("%d undo entries, want 1", len(got.undo))
	}
	if got.stageAnchor == nil {
		t.Fatal("the reader's place was not recorded")
	}
	if !strings.Contains(got.message, "1 line") {
		t.Fatalf("message %q does not say what happened", got.message)
	}
}

// TestUndoStackIsBounded: a long session must not pin a copy of every file it
// touched.
func TestUndoStackIsBounded(t *testing.T) {
	m := testModel(120, 24)
	for i := 0; i < maxUndo+5; i++ {
		next, _ := m.applyStageDone(stageDoneMsg{
			staged: true, op: "stage", lines: 1,
			undo: git.Undo{Path: "a.go", Label: "stage"},
		})
		m = next.(model)
	}
	if len(m.undo) != maxUndo {
		t.Fatalf("%d undo entries, want %d", len(m.undo), maxUndo)
	}
}

func TestUndoOnEmptyStackIsHarmless(t *testing.T) {
	m := testModel(120, 24)
	next, _ := m.undoLast()
	if got := next.(model); !strings.Contains(got.message, "nothing to undo") {
		t.Fatalf("message %q", got.message)
	}
	if len(next.(model).undo) != 0 {
		t.Fatal("an empty stack grew an entry")
	}
}

func TestDoneMessageNamesTheScope(t *testing.T) {
	cases := []struct {
		msg  stageDoneMsg
		want string
	}{
		{stageDoneMsg{op: "stage", lines: 1, scope: "line"}, "staged 1 line (line)"},
		{stageDoneMsg{op: "stage", lines: 4, scope: "hunk"}, "staged 4 lines (hunk)"},
		{stageDoneMsg{op: "unstage", lines: 2, scope: "hunk"}, "unstaged 2 lines (hunk)"},
		{stageDoneMsg{op: "discard", lines: 3, scope: "line"}, "discarded 3 lines (line)"},
		{stageDoneMsg{staged: true, lines: 1}, "staged 1 line (selection)"},
		{stageDoneMsg{op: "stage", path: "a.go", scope: "file"}, "staged a.go"},
	}
	for _, c := range cases {
		if got := doneMessage(c.msg); got != c.want {
			t.Fatalf("got %q, want %q", got, c.want)
		}
	}
}

// TestStageHereWithoutAPaneSaysSo: a keystroke that cannot act should explain
// itself rather than do nothing.
func TestStageHereWithoutAPaneSaysSo(t *testing.T) {
	m := testModel(120, 24)
	next, _ := m.stageHere()
	if !strings.Contains(next.(model).message, "nothing to stage") {
		t.Fatalf("message %q", next.(model).message)
	}
}

// TestDiscardWithoutAPaneSaysSo answers the destructive verb with a message, not a
// dialog, when there is nothing to discard.
func TestDiscardWithoutAPaneSaysSo(t *testing.T) {
	m := testModel(120, 24)
	next, _ := m.discardHere()
	if next.(model).state == stateConfirm {
		t.Fatal("a dialog opened with nothing to discard")
	}
	if !strings.Contains(next.(model).message, "nothing to discard") {
		t.Fatalf("message %q", next.(model).message)
	}
}

// TestConfirmKeys: the confirming key is not the one that opened the dialog,
// and escape always keeps the work.
func TestConfirmKeys(t *testing.T) {
	sel := stage.NewSelection()
	sel.Add(stage.NewPre(1))

	m := testModel(120, 24)
	m.state = stateConfirm
	m.confirm = &confirmState{path: "a.go", sel: sel, lines: []diff.Line{{Kind: diff.Deletion, Text: "-\told()"}}}

	next, _ := m.updateConfirm(keyPress("esc"))
	got := next.(model)
	if got.state != stateShowList {
		t.Fatalf("escape left the dialog open (state %v)", got.state)
	}
	if got.confirm != nil {
		t.Fatal("escape kept the pending discard")
	}
	if !strings.Contains(got.message, "left it alone") {
		t.Fatalf("escape said %q", got.message)
	}

	m.state = stateConfirm
	m.confirm = &confirmState{path: "a.go", sel: sel, lines: []diff.Line{{Kind: diff.Deletion, Text: "-\told()"}}}
	next, cmd := m.updateConfirm(keyPress("enter"))
	got = next.(model)
	if got.state != stateShowList {
		t.Fatalf("confirm left the dialog open (state %v)", got.state)
	}
	if cmd == nil {
		t.Fatal("confirming scheduled nothing")
	}
	if got.confirm != nil {
		t.Fatal("confirming kept the dialog state")
	}
}

// TestSelectModeIsReadFromConfig keeps the two apply models behind one key.
func TestSelectModeIsReadFromConfig(t *testing.T) {
	m := testModel(120, 24)
	m.lastLoaded.path = "a.go"
	m.cfg.StageApply = config.StageApplySelect
	m.syncStaging()
	if !m.view.SelectMode() {
		t.Fatal("select mode did not reach the pane")
	}
	m.cfg.StageApply = config.StageApplyInstant
	m.syncStaging()
	if m.view.SelectMode() {
		t.Fatal("instant mode left selects on in the pane")
	}
}

// TestMarksAreSpentByAnOperation: the marks named lines in a diff that has just
// been rewritten, so carrying them into the next apply would stage lines the
// reader can no longer see.
func TestMarksAreSpentByAnOperation(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.lastLoaded.path = "a.go"
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"one\ntwo\nthree\n", "one\nTWO\nthree\n", diff.GenerateOptions{Context: 3}))
	m.syncStaging()
	m.view.SetSelectMode(true)
	if !m.view.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	for m.view.SelectionScope() != "line" {
		if !m.view.MoveCursor(1) {
			t.Fatal("reach a change line")
		}
	}
	m.view.ToggleMark()
	if m.view.Pending().Empty() {
		t.Fatal("nothing was marked")
	}

	next, _ := m.applyStageDone(stageDoneMsg{
		staged: true, op: "stage", path: "a.go", scope: "line", lines: 2,
		undo: git.Undo{Path: "a.go", Label: "stage"},
	})
	if got := next.(model).view.Pending(); !got.Empty() {
		t.Fatalf("%d marks survived the operation", got.Len())
	}
}

// TestRangeKeyStartsAndCancels: V is a toggle, so a reader who changes their
// mind does not have to find another key to get out.
func TestRangeKeyStartsAndCancels(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.lastLoaded.path = "a.go"
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"one\ntwo\nthree\nfour\n", "ONE\ntwo\nTHREE\nfour\n", diff.GenerateOptions{Context: 1}))
	m.syncStaging()
	if !m.view.MoveCursor(1) {
		t.Fatal("place the cursor")
	}
	next, _ := m.updateKey(keyPress("V"))
	m = next.(model)
	if !m.view.RangeActive() {
		t.Fatal("V did not start a range")
	}
	next, _ = m.updateKey(keyPress("V"))
	if next.(model).view.RangeActive() {
		t.Fatal("V did not cancel the range")
	}
}

// TestDiscardUsesWhatIsMarked: in select mode the marks are the reader's
// selection, so a discard must lose those lines and not whatever the cursor
// happens to be sitting on.
func TestDiscardUsesWhatIsMarked(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.cfg.DiscardConfirm = config.DiscardConfirmNever
	// Select mode comes from the config, which syncStaging reads.
	m.cfg.StageApply = config.StageApplySelect
	m.lastLoaded.path = "a.go"
	m.view.SetFile(diff.Generate("a.go", diff.Modified,
		"a\nb\nc\nd\n", "A\nB\nC\nd\n", diff.GenerateOptions{Context: 0}))
	m.syncStaging()
	m.view.ClearRange()

	// Mark the whole hunk, then walk to one line: the cursor and the marks now
	// disagree, and the marks are what the reader built.
	if !m.view.CursorTo(1) {
		t.Fatalf("the hunk header is not at row 1 of %d rows", m.view.YOffset())
	}
	if got := m.view.SelectionScope(); got != "hunk" {
		t.Fatalf("scope %q at the hunk header, want hunk", got)
	}
	m.view.ToggleMark()
	marked := m.view.Pending().Len()
	if marked != 6 {
		t.Fatalf("marked %d lines, want the three replacements", marked)
	}
	for m.view.SelectionScope() != "line" {
		if !m.view.MoveCursor(1) {
			t.Fatal("reach a change line")
		}
	}

	next, cmd := m.discardHere()
	if cmd == nil {
		t.Fatalf("discard produced nothing: %q", next.(model).message)
	}
	msg, ok := cmd().(stageDoneMsg)
	if !ok {
		t.Fatal("the command did not produce a stageDoneMsg")
	}
	if msg.op != "discard" {
		t.Fatalf("operation %q, want discard", msg.op)
	}
	if msg.lines != marked {
		t.Fatalf("discard covers %d lines, want the %d marked ones", msg.lines, marked)
	}
}

// TestStagingInAllChangesKeepsTheDocument: the pane holds every file, so the
// immediate reload must rebuild the document rather than replace it with the one
// file the cursor was in.
func TestStagingInAllChangesKeepsTheDocument(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.allChanges = true
	m.files.SetFiles([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil)
	docFile := diff.Generate("a.go", diff.Modified, "one\ntwo\nthree\n", "one\nTWO\nthree\n", diff.GenerateOptions{Context: 1})
	// The single-file diff is already cached, which is what used to make the
	// reload swap the document out for it.
	m.cache.Put(fileCacheKey("a.go", false, m.cfg), cacheEntry{file: docFile})
	m.view.SetDocument(func(int) *diff.File { return docFile })
	m.view.AppendBlocks([]*diff.File{docFile})
	m.syncStaging()

	blocks, _ := m.view.DocBlocks()
	if blocks == 0 {
		t.Fatal("the fixture has no document")
	}
	next, cmd := m.applyStageDone(stageDoneMsg{
		op: "stage", path: "a.go", scope: "line", lines: 2,
		undo: git.Undo{Path: "a.go", Label: "stage"},
	})
	if cmd == nil {
		t.Fatal("no reload was scheduled")
	}
	got := next.(model)
	if !got.allChanges {
		t.Fatal("the all-changes view was left")
	}
	// The document is rebuilt from the listing, so no file is loaded on its
	// own: the pane keeps an empty lastLoaded and no single file.
	if got.lastLoaded.path != "" {
		t.Fatalf("the pane loaded %q as a single file", got.lastLoaded.path)
	}
	if got.view.File() != nil {
		t.Fatal("the document was replaced by a single file")
	}
}

// TestDiscardDialogScrollsWithTheDiffKeys: the preview scrolls with the keys the
// diff pane already scrolls with, so a long discard can be read through. The
// arrows belong to that set, and they still move the staging cursor outside the
// dialog.
func TestDiscardDialogScrollsWithTheDiffKeys(t *testing.T) {
	m := testModel(120, 24)
	m.state = stateConfirm
	sel := stage.NewSelection()
	lines := make([]diff.Line, 40)
	for i := range lines {
		lines[i] = diff.Line{Kind: diff.Deletion, Text: "-line"}
		if i == 0 {
			// One line wider than the dialog, so sideways scrolling has
			// somewhere to go.
			lines[i].Text = "-" + strings.Repeat("wide ", 40)
		}
		sel.Add(stage.NewPre(i + 1))
	}
	m.confirm = &confirmState{path: "a.go", sel: sel, lines: lines}

	arrow := func(b key.Binding, want string) {
		t.Helper()
		found := false
		for _, k := range b.Keys() {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("binding %v does not take %q", b.Keys(), want)
		}
	}
	arrow(m.keys.CursorUp, "up")
	arrow(m.keys.CursorDown, "down")
	arrow(m.keys.ScrollLeft, "left")
	arrow(m.keys.ScrollRight, "right")

	rows := m.confirmRows()
	if len(lines) <= rows {
		t.Fatalf("%d rows fit %d lines; the test needs a scrollable preview", rows, len(lines))
	}

	step := func(s string) {
		t.Helper()
		next, _ := m.updateConfirm(keyPress(s))
		m = next.(model)
	}
	step("j")
	step("down")
	if got := m.confirm.off; got != 2 {
		t.Fatalf("after two lines the preview starts at %d, want 2", got)
	}
	step("b")
	if got := m.confirm.off; got != 0 {
		t.Fatalf("page up left the preview at %d, want 0", got)
	}
	step("f")
	if got := m.confirm.off; got != rows {
		t.Fatalf("page down left the preview at %d, want %d", got, rows)
	}
	// Paging past the end stops on the last full window instead of blanking.
	for range 5 {
		step("f")
	}
	if got, want := m.confirm.off, m.confirm.maxOffset(rows); got != want {
		t.Fatalf("preview starts at %d, want the last window %d", got, want)
	}

	// Sideways: a long line can be read to its end, and then stops.
	step("l")
	if got := m.confirm.x; got != 4 {
		t.Fatalf("horizontal offset %d after one step, want 4", got)
	}
	for range 100 {
		step("l")
	}
	if got, want := m.confirm.x, m.confirm.maxX(m.confirmInnerWidth()); got != want {
		t.Fatalf("horizontal offset %d, want it clamped to %d", got, want)
	}
	step("h")
	if got := m.confirm.x; got != max(0, m.confirm.maxX(m.confirmInnerWidth())-4) {
		t.Fatalf("horizontal offset %d after scrolling back, want 4 less", got)
	}

	if next, _ := m.updateConfirm(keyPress("esc")); next.(model).state != stateShowList {
		t.Fatal("escape did not close the dialog")
	}
}

// TestEachRowLoadsItsOwnSide: a file that is staged and edited is two rows, and
// the row the reader picked is the diff the pane shows. Nothing is hidden and
// nothing has to be flipped.
func TestEachRowLoadsItsOwnSide(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(
		[]git.FileStatus{{Path: "both.go", Letter: "M", Staged: true}},
		[]git.FileStatus{{Path: "both.go", Letter: "M"}},
	)

	m.files.SelectSide("both.go", true)
	if path, staged := m.selectedSide(); path != "both.go" || !staged {
		t.Fatalf("the staged row loaded %q staged=%v", path, staged)
	}
	m.files.SelectSide("both.go", false)
	if path, staged := m.selectedSide(); path != "both.go" || staged {
		t.Fatalf("the unstaged row loaded %q staged=%v", path, staged)
	}
}

// TestAllChangesOpensOnTheSelectedRow: the document's bands are the listing's
// rows in order, so selecting the tenth file has to scroll the document to the
// tenth block and put the cursor in it. A band list that drifts from the
// listing makes the sidebar and the pane disagree about which file is on
// screen, and every jump after the drift lands on the wrong file.
func TestAllChangesOpensOnTheSelectedRow(t *testing.T) {
	const n = 12
	m := testModel(120, 24)
	m.focus = focusMain
	statuses := make([]git.FileStatus, n)
	for i := range statuses {
		statuses[i] = git.FileStatus{Path: fmt.Sprintf("f%d.txt", i+1), Letter: "M"}
	}
	m.files.SetFiles(nil, statuses)
	entries := m.files.OrderedEntries()
	if len(entries) != n {
		t.Fatalf("%d rows, want %d", len(entries), n)
	}
	files := make([]*diff.File, n)
	for i, e := range entries {
		files[i] = diff.Generate(e.Status.Path, diff.Modified, "a\nb\nc\n", "a\nCHANGED\nc\n",
			diff.GenerateOptions{Context: 1})
	}

	// The reader picks a row well past the first screen and asks for the
	// whole change set.
	want := 7
	if !m.files.SelectSide(entries[want].Status.Path, entries[want].Staged) {
		t.Fatal("the row to select is missing from the listing")
	}
	// In the app the pane is a staging pane by the time a chunk arrives, which
	// is what lets the pending jump place the cursor.
	m.allChanges = true
	m.syncStaging()
	next, _ := m.startAllChanges()
	m = next.(model)
	next, _ = m.update(allChunkMsg{
		entries: entries,
		files:   files,
		gen:     m.allGen,
		done:    true,
	})
	m = next.(model)
	t.Logf("after handler: y=%d cursor=%d band=%d", m.view.YOffset(), m.view.CursorRow(), m.view.BandRow(want))

	if blocks, _ := m.view.DocBlocks(); blocks != n {
		t.Fatalf("the document holds %d blocks for %d rows", blocks, n)
	}
	band := m.view.BandRow(want)
	if band <= 0 {
		t.Fatalf("band %d of %d is at row %d", want, n, band)
	}
	if got := m.view.YOffset(); got != band {
		t.Fatalf("the document is at %d, want the selected row's band at %d", got, band)
	}
	if row := m.view.CursorRow(); row < band || (want+1 < n && row >= m.view.BandRow(want+1)) {
		t.Fatalf("cursor on row %d, want inside the block at %d", row, band)
	}
}

// TestDocumentCarriesEachRowsSide: the all-changes document holds one block per
// row, so the same path can have a staged block and an unstaged one, and the
// side a keystroke acts on comes from the block the cursor is in.
func TestDocumentCarriesEachRowsSide(t *testing.T) {
	m := testModel(120, 24)
	m.focus = focusMain
	m.allChanges = true
	m.files.SetFiles(
		[]git.FileStatus{{Path: "both.go", Letter: "M", Staged: true}},
		[]git.FileStatus{{Path: "both.go", Letter: "M"}},
	)
	staged := diff.Generate("both.go", diff.Modified, "one\n", "ONE\n", diff.GenerateOptions{Context: 1})
	unstaged := diff.Generate("both.go", diff.Modified, "ONE\n", "ONE!\n", diff.GenerateOptions{Context: 1})
	entries := m.files.OrderedEntries()
	if len(entries) != 2 {
		t.Fatalf("%d rows, want one per side", len(entries))
	}
	m.view.SetDocument(func(int) *diff.File { return nil })
	m.view.AppendBlocksSided([]*diff.File{staged, unstaged}, blockSides(m.cfg, entries, 0, 2))
	m.syncStaging()

	// The staged block comes first, so the cursor starts there.
	if got, ok := m.view.CursorStaged(); !ok || !got {
		t.Fatalf("the first block reads staged=%v ok=%v", got, ok)
	}
	// Walk into the second block and the side follows.
	for i := 0; i < 40; i++ {
		if !m.view.MoveCursor(1) {
			break
		}
		if row := m.view.CursorRow(); row >= m.view.BandRow(1) {
			break
		}
	}
	if got, ok := m.view.CursorStaged(); !ok || got {
		t.Fatalf("the second block reads staged=%v ok=%v", got, ok)
	}
	if path, side, ok := m.cursorTarget(); path != "both.go" || side || !ok {
		t.Fatalf("cursorTarget = %q %v %v, want the worktree side of both.go", path, side, ok)
	}
}

// TestBlockSidesLabelTheHalves: a file with a change staged and another left in the
// worktree is listed twice, so its two blocks have to carry the label that tells them
// apart, in whichever of the three forms the reader asked for.
func TestBlockSidesLabelTheHalves(t *testing.T) {
	entries := []filelist.Entry{
		{Status: git.FileStatus{Path: "both.go"}, Staged: true},
		{Status: git.FileStatus{Path: "solo.go"}},
		{Status: git.FileStatus{Path: "both.go"}},
	}
	for _, tc := range []struct {
		mode             string
		staged, worktree string
	}{
		{config.SideLabelsText, "  \u00b7 staged", "  \u00b7 unstaged"},
		{config.SideLabelsGlyph, "  \u25b2", "  \u25c6"},
		{config.SideLabelsOff, "", ""},
		// An unknown value falls back to the default, which is the text.
		{"nonsense", "  \u00b7 staged", "  \u00b7 unstaged"},
	} {
		cfg := config.Default()
		cfg.SideLabels = tc.mode
		got := blockSides(cfg, entries, 0, len(entries))
		if len(got) != 3 {
			t.Fatalf("%s: %d sides for %d rows", tc.mode, len(got), len(entries))
		}
		if !got[0].Staged || got[0].Tag != tc.staged {
			t.Fatalf("%s: staged row = %+v, want tag %q", tc.mode, got[0], tc.staged)
		}
		if got[1].Staged || got[1].Tag != "" {
			t.Fatalf("%s: single row = %+v, want a bare worktree block", tc.mode, got[1])
		}
		if got[2].Staged || got[2].Tag != tc.worktree {
			t.Fatalf("%s: worktree row = %+v, want tag %q", tc.mode, got[2], tc.worktree)
		}
	}
}

// TestLastChangeGoneSaysSo: staging or discarding the last line of a file makes
// its diff empty, which is not a failure and must not be shown as one.
func TestLastChangeGoneSaysSo(t *testing.T) {
	m := testModel(120, 24)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.seq = 7
	next, _ := m.Update(fileLoadedMsg{
		path: "a.go",
		seq:  7,
		err:  fmt.Errorf("%w for %q", git.ErrNoChange, "a.go"),
	})
	got := next.(model)
	if !strings.Contains(got.message, "nothing left") {
		t.Fatalf("message %q does not say the file is done", got.message)
	}
	if got.lastLoaded.path != "" {
		t.Fatalf("the pane still thinks %q is loaded", got.lastLoaded.path)
	}
	if got.view.File() != nil {
		t.Fatal("the pane kept a diff that no longer exists")
	}
}
