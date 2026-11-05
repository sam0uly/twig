package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/stage"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// edited is a diff of a 40-line file where the given lines were rewritten, which
// is what an agent editing a file looks like to the viewer.
func edited(edits map[int]string) *diff.File {
	var before, after strings.Builder
	for i := 1; i <= 40; i++ {
		if text, ok := edits[i]; ok {
			before.WriteString("original\n")
			after.WriteString(text + "\n")
			continue
		}
		before.WriteString("same\n")
		after.WriteString("same\n")
	}
	return diff.Generate("a.go", diff.Modified, before.String(), after.String(),
		diff.GenerateOptions{Context: 3})
}

// TestFollowedChangeIsTheNewLine: following has to name the line that changed,
// not the first hunk and not the file's first change.
func TestFollowedChangeIsTheNewLine(t *testing.T) {
	prev := edited(map[int]string{10: "first edit"})
	next := edited(map[int]string{10: "first edit", 30: "second edit"})

	line, ok := followedChange(prev, next)
	if !ok {
		t.Fatal("the new edit was not found")
	}
	if line.New != 30 || line.Text != "+second edit" {
		t.Fatalf("followed %+v, want line 30's edit", line)
	}
}

// TestFollowedChangeSeesAnEditedLineAgain: the same line number with different
// text is a different line, so a second edit to the same place is followed too.
func TestFollowedChangeSeesAnEditedLineAgain(t *testing.T) {
	line, ok := followedChange(edited(map[int]string{10: "first try"}), edited(map[int]string{10: "second try"}))
	if !ok {
		t.Fatal("the rewrite of the same line was not followed")
	}
	if line.New != 10 || line.Text != "+second try" {
		t.Fatalf("followed %+v, want line 10 rewritten", line)
	}
}

// TestFollowedChangeIgnoresWhatWasAlreadyThere: nothing new means nothing to
// follow, so an unrelated status refresh never moves the reader.
func TestFollowedChangeIgnoresWhatWasAlreadyThere(t *testing.T) {
	f := edited(map[int]string{10: "first edit", 30: "second edit"})
	if _, ok := followedChange(f, edited(map[int]string{10: "first edit", 30: "second edit"})); ok {
		t.Fatal("an unchanged diff reported a change")
	}
}

// TestLiveReloadFollowsTheChange is the wiring: a refresh that brings in a new
// line moves the cursor onto it, with the pane scrolled so it sits in context.
func TestLiveReloadFollowsTheChange(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.files.SelectSide("a.go", false)
	m.cfg.Watch, m.cfg.Follow = true, true

	first := edited(map[int]string{10: "first edit"})
	m.view.SetFile(first)
	m.rememberDiff("a.go", first, false)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.view.JumpToLine(0)
	m.syncStaging()

	second := edited(map[int]string{10: "first edit", 30: "second edit"})
	next, _ := m.Update(fileLoadedMsg{file: second, path: "a.go", seq: m.seq})
	nm := next.(model)

	row := nm.view.CursorRow()
	if row < 0 {
		t.Fatal("no cursor after the live reload")
	}
	if sel := nm.view.Selection(); !sel.Has(stage.NewPost(30)) || !sel.Has(stage.NewPre(30)) {
		t.Fatalf("cursor names %v, want the line that just changed", sel.Lines())
	}
	if height := row - nm.view.YOffset(); height < 1 {
		t.Fatalf("the changed line sits at the top of the pane (%d), want context above it", height)
	}
}

// TestFirstChangeOfACleanFileIsTheEditedLine: when a clean file gains its first
// change, the whole diff is new, so the line to chase is the line that changed,
// not the first row of the hunk.
func TestFirstChangeOfACleanFileIsTheEditedLine(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "clean.go", Letter: "M"}})
	m.files.SelectSide("clean.go", false)
	m.cfg.Watch, m.cfg.Follow = true, true
	m.lastLoaded.path = "clean.go"

	next := edited(map[int]string{30: "FIRST"})
	m.view.SetFile(next) // nothing was shown before this, so there is no delta
	m.syncStaging()
	m.followShown("clean.go", false, next)

	if row := m.view.CursorRow(); row < 0 {
		t.Fatal("no cursor after the follow")
	}
	if sel := m.view.Selection(); !sel.Has(stage.NewPost(30)) {
		t.Fatalf("cursor names %v, want the new line 30", sel.Lines())
	}
}

// TestFollowLeavesOwnWorkAlone: a reload caused by the reader's own staging keeps
// the reader where they were, which is what the anchor is for.
func TestFollowLeavesOwnWorkAlone(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.files.SelectSide("a.go", false)
	m.cfg.Watch, m.cfg.Follow = true, true

	first := edited(map[int]string{10: "first edit"})
	m.view.SetFile(first)
	m.rememberDiff("a.go", first, false)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.view.JumpToLine(20)
	before := m.view.YOffset()

	next, _ := m.applyStageDone(stageDoneMsg{
		op: "stage", path: "a.go", scope: "line", lines: 1,
		undo: git.Undo{Path: "a.go", Label: "stage"},
	})
	m = next.(model)
	if !m.anchorWait {
		t.Fatal("the staging anchor was not armed")
	}
	second := edited(map[int]string{10: "first edit", 30: "second edit"})
	next, _ = m.Update(fileLoadedMsg{file: second, path: "a.go", seq: m.seq})
	if got := next.(model).view.YOffset(); got != before {
		t.Fatalf("own staging moved the view to %d, want %d", got, before)
	}
}

// TestFollowOffKeepsTheReaderInPlace covers the setting: with follow off, a live
// reload redraws at the reader's line and goes nowhere near the edit.
func TestFollowOffKeepsTheReaderInPlace(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.files.SelectSide("a.go", false)
	m.cfg.Watch, m.cfg.Follow = true, false

	first := edited(map[int]string{10: "first edit"})
	m.view.SetFile(first)
	m.rememberDiff("a.go", first, false)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.view.JumpToLine(20)
	before := m.view.YOffset()

	second := edited(map[int]string{10: "first edit", 30: "second edit"})
	next, _ := m.Update(fileLoadedMsg{file: second, path: "a.go", seq: m.seq})
	if got := next.(model).view.YOffset(); got != before {
		t.Fatalf("follow off moved the view to %d, want %d", got, before)
	}
}

// TestLiveChangeInAnotherFileIsChosen: the follow is per repository, not per
// open file. A file whose bytes moved is picked out of the listing, and a file
// whose stat did not move is left alone.
func TestLiveChangeInAnotherFileIsChosen(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Watch, m.cfg.Follow = true, true

	dir := t.TempDir()
	calm := filepath.Join(dir, "calm.go")
	moved := filepath.Join(dir, "moved.go")
	for _, p := range []string{calm, moved} {
		if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	entries := []filelist.Entry{
		{Status: git.FileStatus{Path: calm, Letter: "M"}},
		{Status: git.FileStatus{Path: moved, Letter: "M"}},
	}
	if got := m.changedFiles(entries); len(got) != 0 {
		t.Fatalf("the first look reported %d changes", len(got))
	}

	if err := os.WriteFile(moved, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	changed := m.changedFiles(entries)
	if len(changed) != 1 || changed[0].Status.Path != moved {
		t.Fatalf("changed %v, want just %s", changed, moved)
	}
	pick, ok := m.liveFollow(changed)
	if !ok || pick.Status.Path != moved {
		t.Fatalf("follow picked %+v, %v", pick, ok)
	}
}

// TestCleanFileEnteringTheListingIsFollowed: a file that was clean has no
// snapshot, and it appears in the listing the moment it is edited. That is the
// case the follow exists for, so entering the list counts as a change.
func TestCleanFileEnteringTheListingIsFollowed(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Watch, m.cfg.Follow = true, true
	dir := t.TempDir()
	busy := filepath.Join(dir, "busy.go")
	fresh := filepath.Join(dir, "fresh.go")
	if err := os.WriteFile(busy, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := []filelist.Entry{{Status: git.FileStatus{Path: busy, Letter: "M"}}}
	if got := m.changedFiles(start); len(got) != 0 {
		t.Fatalf("the first look reported %d changes", len(got))
	}

	joined := []filelist.Entry{
		{Status: git.FileStatus{Path: busy, Letter: "M"}},
		{Status: git.FileStatus{Path: fresh, Letter: "M"}},
	}
	changed := m.changedFiles(joined)
	if len(changed) != 1 || changed[0].Status.Path != fresh {
		t.Fatalf("changed %v, want the file that just appeared", changed)
	}
}

// TestFollowIsOffWhenTheSettingIsOff: no chase without the setting, however much
// the listing moved.
func TestFollowIsOffWhenTheSettingIsOff(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Watch, m.cfg.Follow = true, false
	entries := []filelist.Entry{{Status: git.FileStatus{Path: "a.go", Letter: "M"}}}
	m.changedFiles(entries)
	if _, ok := m.liveFollow(entries); ok {
		t.Fatal("follow picked a file with the setting off")
	}
}

// TestFollowDoesNotConsumeChangesWhileStaging: the baselines must stay put while
// the reader's own operation is reloading, or a change that arrived meanwhile is
// measured away and never followed.
func TestFollowDoesNotConsumeChangesWhileStaging(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Watch, m.cfg.Follow = true, true
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := []filelist.Entry{{Status: git.FileStatus{Path: p, Letter: "M"}}}
	m.changedFiles(entries)

	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.anchorWait = true
	if got := m.changedFiles(entries); len(got) != 0 {
		t.Fatalf("a staging reload chased %d files", len(got))
	}
	m.anchorWait = false
	if got := m.changedFiles(entries); len(got) != 1 {
		t.Fatalf("the change was measured away: %d", len(got))
	}
}

// TestFollowedChangeSeesADuplicateLine: two identical added lines are two
// identities, so appending a second one to the changed set is still a change.
func TestFollowedChangeSeesADuplicateLine(t *testing.T) {
	build := func(n int) *diff.File {
		var before, after strings.Builder
		before.WriteString("a\nb\nc\n")
		after.WriteString("a\nb\nc\n")
		for i := 0; i < n; i++ {
			after.WriteString("}\n")
		}
		return diff.Generate("a.go", diff.Modified, before.String(), after.String(),
			diff.GenerateOptions{Context: 3})
	}
	line, ok := followedChange(build(1), build(2))
	if !ok {
		t.Fatal("the second identical line was not followed")
	}
	if line.Text != "+}" {
		t.Fatalf("followed %q, want the added brace", line.Text)
	}
}

// TestFollowRestoresTheFileWhenNothingChanged: a stat that moved without an edit
// (a touch, a save of identical bytes) must not carry the reader away from what
// they were reading.
func TestFollowRestoresTheFileWhenNothingChanged(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetFiles(nil, []git.FileStatus{
		{Path: "a.go", Letter: "M"},
		{Path: "b.go", Letter: "M"},
	})
	m.files.SelectSide("a.go", false)
	m.cfg.Watch, m.cfg.Follow = true, true

	shown := edited(map[int]string{10: "first edit"})
	m.view.SetFile(shown)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.syncStaging()

	// The chase picked b.go, but its diff is the one it already had: the
	// stat moved and the content did not.
	m.rememberDiff("b.go", edited(map[int]string{10: "first edit"}), false)
	m.followBack = sideRef{path: "a.go", ok: true}
	next, cmd := m.Update(fileLoadedMsg{file: edited(map[int]string{10: "first edit"}), path: "b.go", seq: m.seq})
	nm := next.(model)
	if cmd == nil {
		t.Fatal("no reload was scheduled for the file to go back to")
	}
	msg, ok := cmd().(fileLoadedMsg)
	if !ok {
		t.Fatalf("the way back loaded %T, want a file diff", cmd())
	}
	if msg.path != "a.go" {
		t.Fatalf("went back to %q, want a.go", msg.path)
	}
	if nm.lastLoaded.path != "" {
		t.Fatalf("lastLoaded = %q, want it cleared while the way back loads", nm.lastLoaded.path)
	}
}

// TestDiffOptionChangeForgetsSnapshots: a diff taken with other options cannot
// be compared with one taken with these, so the remembered diffs are dropped
// (the stat baselines stay, so changes are still detected).
func TestDiffOptionChangeForgetsSnapshots(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Follow, m.cfg.Watch = true, true
	f := edited(map[int]string{10: "first edit"})
	m.rememberDiff("a.go", f, false)
	if len(m.liveNews) != 1 {
		t.Fatalf("%d snapshots, want 1", len(m.liveNews))
	}

	cfg := m.cfg
	cfg.Syntax = !cfg.Syntax
	if cmd := m.applyConfig(cfg); cmd != nil {
		t.Fatal("toggling syntax should not reload content")
	}

	cfg.Context = m.cfg.Context + 1
	_ = m.applyConfig(cfg)
	for path, entry := range m.liveNews {
		if entry.diff != nil {
			t.Fatalf("%v kept a diff taken with the old options", path)
		}
	}
}

// TestChangedFilesNamesTheWorktreeRow: a staged row is fingerprinted from the
// same worktree file, so its numbers move whenever the file is written. Only
// the worktree row is a change, or the follow chases an index diff that did not
// move.
func TestChangedFilesNamesTheWorktreeRow(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Follow, m.cfg.Watch = true, true

	entries := func(size, mtime int64) []filelist.Entry {
		return []filelist.Entry{
			{Status: git.FileStatus{Path: "a.go", Size: size, ModTime: mtime}, Staged: true},
			{Status: git.FileStatus{Path: "a.go", Size: size, ModTime: mtime}},
		}
	}
	// The first load only records: nothing has changed before there is
	// something to compare with.
	if got := m.changedFiles(entries(10, 1)); len(got) != 0 {
		t.Fatalf("the first load reported %+v", got)
	}

	// The file is written. Both rows moved, because both were stamped from the
	// same file, and only the worktree one has anything to show for it.
	got := m.changedFiles(entries(20, 2))
	if len(got) != 1 {
		t.Fatalf("changed files = %+v, want one", got)
	}
	if got[0].Staged {
		t.Fatalf("followed the staged row of %q, which the write did not touch", got[0].Status.Path)
	}
}

// TestAllChangesFollowsTheChosenSide: the document holds a partly staged file
// twice, so the follow has to compare the side as well as the path. A staged
// block that consumes the follow lands the cursor in the half that did not
// change, or swallows the follow entirely.
func TestAllChangesFollowsTheChosenSide(t *testing.T) {
	const path = "semi.txt"
	const base = "one\ntwo\nthree\nfour\nfive\n"
	const index = "ONE\nTWO-STAGED\nthree\nfour\nfive\n"
	gen := func(before, after string) *diff.File {
		return diff.Generate(path, diff.Modified, before, after, diff.GenerateOptions{Context: 1})
	}

	// The staged half gained a line since it was last seen, and the worktree
	// half gained another one: only the second is the change being followed.
	stagedWas := gen(base, "ONE\ntwo\nthree\nfour\nfive\n")
	stagedNow := gen(base, index)
	worktreeWas := gen(index, "ONE\nTWO-STAGED\nthree\nfour\nFIVE\n")
	worktreeNow := gen(index, "ONE\nTWO-STAGED\nthree\nfour\nFIVE-WORKTREE\n")

	m := testModel(120, 24)
	m.focus = focusMain
	m.allChanges = true
	m.cfg.Follow, m.cfg.Watch = true, true
	m.files.SetFiles(
		[]git.FileStatus{{Path: path, Letter: "M"}},
		[]git.FileStatus{{Path: path, Letter: "M"}},
	)
	// The sidebar is left on the staged row on purpose: the side to follow
	// comes from the chase, not from whatever the list happens to highlight.
	m.files.SelectSide(path, true)
	m.rememberDiff(path, stagedWas, true)
	m.rememberDiff(path, worktreeWas, false)
	m.allFollow, m.allFollowStaged = path, false

	m.view.SetDocument(func(i int) *diff.File {
		if i == 0 {
			return stagedNow
		}
		return worktreeNow
	})
	next, _ := m.Update(allChunkMsg{
		entries: m.files.AllEntries(),
		files:   []*diff.File{stagedNow, worktreeNow},
		gen:     m.allGen,
		done:    true,
	})
	m = next.(model)

	staged, ok := m.view.CursorStaged()
	if !ok {
		t.Fatal("the cursor is not in any block")
	}
	if staged {
		t.Fatal("the cursor landed in the staged half, which did not change")
	}
	sel := m.view.Selection()
	if sel.Empty() {
		t.Fatal("the follow left the cursor on nothing")
	}
	if !sel.Has(stage.NewPost(5)) {
		t.Fatalf("cursor names %v, want the line that was just written", sel.Lines())
	}
}

// TestFollowedChangeIsTheLastEdit: a tool that rewrites a file in several places
// leaves the reader where it stopped. Following the first new line instead left
// the cursor on an edit the tool had already moved past.
func TestFollowedChangeIsTheLastEdit(t *testing.T) {
	prev := edited(map[int]string{10: "first"})
	next := edited(map[int]string{10: "first", 20: "second", 34: "third"})
	line, ok := followedChange(prev, next)
	if !ok {
		t.Fatal("the burst was not followed")
	}
	if line.New != 34 || line.Text != "+third" {
		t.Fatalf("followed %+v, want the last edit on line 34", line)
	}
}

// TestLiveFollowTakesTheNewestFile: a burst can touch several files, and the one
// written last is where the tool is.
func TestLiveFollowTakesTheNewestFile(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Follow, m.cfg.Watch = true, true
	changed := []filelist.Entry{
		{Status: git.FileStatus{Path: "a.go", Size: 1, ModTime: 100}},
		{Status: git.FileStatus{Path: "b.go", Size: 1, ModTime: 300}},
		{Status: git.FileStatus{Path: "c.go", Size: 1, ModTime: 200}},
	}
	got, ok := m.liveFollow(changed)
	if !ok {
		t.Fatal("no file was chosen")
	}
	if got.Status.Path != "b.go" {
		t.Fatalf("followed %q, want the file written last", got.Status.Path)
	}

	// The file on screen wins a tie: the reader is already looking at it.
	m.lastLoaded.path = "c.go"
	changed[1].Status.ModTime = 200
	if got, _ := m.liveFollow(changed); got.Status.Path != "c.go" {
		t.Fatalf("followed %q on a tie, want the file on screen", got.Status.Path)
	}
}

// TestSameRows: a live change patches a block when every row the listing shows
// already has one, and rebuilds when the set of rows itself changed.
func TestSameRows(t *testing.T) {
	row := func(path string, staged bool) filelist.Entry {
		return filelist.Entry{Status: git.FileStatus{Path: path}, Staged: staged}
	}
	a := []filelist.Entry{row("a.go", true), row("a.go", false), row("b.go", false)}
	if !sameRows(a, a) {
		t.Fatal("the same rows were not recognised")
	}
	if sameRows(a, []filelist.Entry{row("a.go", true), row("a.go", false)}) {
		t.Fatal("a shorter listing looked the same")
	}
	if sameRows(a, []filelist.Entry{row("a.go", false), row("a.go", true), row("b.go", false)}) {
		t.Fatal("a reordered listing looked the same")
	}
	if sameRows(nil, nil) {
		t.Fatal("two empty listings were called a patch")
	}
}

// bigDiff is a whole-file rewrite: every line gone and every line new.
func bigDiff(changes int) *diff.File {
	var before, after strings.Builder
	for i := 0; i < changes/2; i++ {
		before.WriteString("was here\n")
		after.WriteString("is here\n")
	}
	return diff.Generate("a.go", diff.Modified, before.String(), after.String(),
		diff.GenerateOptions{Context: 3})
}

// TestRememberDiffKeepsABigDiffOutOfTheModel: a baseline is a convenience, and a
// rewrite of thousands of lines is not a line anyone chases. Its stat has to stay,
// or the next change reads as a new row.
func TestRememberDiffKeepsABigDiffOutOfTheModel(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Follow = true
	m.rememberDiff("a.go", bigDiff(maxLiveRows+2), false)
	entry := m.liveNews[livePath{path: "a.go"}]
	if entry == nil {
		t.Fatal("the stat baseline went with the diff")
	}
	if entry.diff != nil {
		t.Fatalf("kept %d changed lines, want a baseline under %d",
			entry.diff.Additions+entry.diff.Deletions, maxLiveRows)
	}
	if got := m.heldDiffs(); got != 0 {
		t.Fatalf("%d baselines held, want none", got)
	}
}

// TestRememberDiffHoldsFewBaselines: a burst that touches many files keeps every
// stat but only a few sets of rows, so the model cannot grow by everything that
// moved.
func TestRememberDiffHoldsFewBaselines(t *testing.T) {
	m := testModel(120, 24)
	m.cfg.Follow = true
	for i := 0; i <= maxLiveDiffs; i++ {
		m.rememberDiff(fmt.Sprintf("f%d.go", i), edited(map[int]string{10: "edit"}), false)
	}
	if got := m.heldDiffs(); got != maxLiveDiffs {
		t.Fatalf("%d baselines held, want %d", got, maxLiveDiffs)
	}
	if got := len(m.liveNews); got != maxLiveDiffs+1 {
		t.Fatalf("%d stat baselines, want one per file", got)
	}
}
