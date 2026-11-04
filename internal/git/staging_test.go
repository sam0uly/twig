package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
)

// worktreeDiff is the diff the staging view shows for a path, which is also
// where line selections come from.
func worktreeDiff(t *testing.T, path string) *stage.Selection {
	t.Helper()
	f, err := FileDiff(path, false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff(%s): %v", path, err)
	}
	return stage.AllSelection(f)
}

// lineSelection picks the change at one line number: the deletion on the old
// side and the addition on the new side, which is what a cursor on a replaced
// line selects.
func lineSelection(pre, post int) *stage.Selection {
	sel := stage.NewSelection()
	if pre > 0 {
		sel.Add(stage.NewPre(pre))
	}
	if post > 0 {
		sel.Add(stage.NewPost(post))
	}
	return sel
}

// TestStageOneLineOfTwo writes one of two replaced lines into the index and
// checks the result the way git sees it, not the way twig thinks it went.
func TestStageOneLineOfTwo(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\nfour\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nTHREE\nfour\n")

	if got, want := worktreeDiff(t, "f.txt").Len(), 4; got != want {
		t.Fatalf("%d change lines, want %d", got, want)
	}

	if _, err := StageLines("f.txt", lineSelection(2, 2)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got, want := h.gitOutput("show", ":f.txt"), "one\nTWO\nthree\nfour\n"; got != want {
		t.Fatalf("index holds %q, want %q", got, want)
	}
	// The unselected change is still only in the worktree.
	if got := h.gitOutput("diff", "--cached", "--", "f.txt"); !strings.Contains(got, "+TWO") || strings.Contains(got, "THREE") {
		t.Fatalf("staged diff is wrong:\n%s", got)
	}
	if got := h.gitOutput("diff", "--", "f.txt"); !strings.Contains(got, "+THREE") || strings.Contains(got, "+TWO") {
		t.Fatalf("worktree diff is wrong:\n%s", got)
	}
	h.git("fsck", "--no-dangling")
}

// TestStageThenUnstageReturns the index to exactly where it started.
func TestStageThenUnstageReturns(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\nfour\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nTHREE\nfour\n")

	if _, err := StageLines("f.txt", lineSelection(2, 2)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	// The staged diff names the same line numbers, so the same selection takes
	// it back out.
	staged, err := FileDiff("f.txt", true, Options{Context: 3})
	if err != nil {
		t.Fatalf("staged FileDiff: %v", err)
	}
	sel := stage.AllSelection(staged)
	if _, err := UnstageLines("f.txt", sel); err != nil {
		t.Fatalf("UnstageLines: %v", err)
	}
	if got, want := h.gitOutput("show", ":f.txt"), "one\ntwo\nthree\nfour\n"; got != want {
		t.Fatalf("index holds %q, want %q", got, want)
	}
	if got := h.gitOutput("diff", "--cached", "--", "f.txt"); got != "" {
		t.Fatalf("staged diff should be empty, got:\n%s", got)
	}
}

// TestStageUndoIsExact replays the recorded undo and checks the index returns
// to the byte it started at.
func TestStageUndoIsExact(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "a\nb\nc\n")
	h.commitAll("base")
	h.write("f.txt", "a\nB\nc\n")

	res, err := StageLines("f.txt", lineSelection(2, 2))
	if err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got := h.gitOutput("show", ":f.txt"); got != "a\nB\nc\n" {
		t.Fatalf("index holds %q before undo", got)
	}
	if err := res.Undo.Apply(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := h.gitOutput("show", ":f.txt"); got != "a\nb\nc\n" {
		t.Fatalf("index holds %q after undo", got)
	}
}

// TestStageOnTopOfStagedWork stages on top of an already staged file: the patch
// base is the index, not HEAD.
func TestStageOnTopOfStagedWork(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "1\n2\n3\n4\n5\n")
	h.commitAll("base")

	h.write("f.txt", "1\nTWO\n3\n4\n5\n")
	h.git("add", "f.txt") // first line staged
	h.write("f.txt", "1\nTWO\n3\nFOUR\n5\n")

	if _, err := StageLines("f.txt", lineSelection(0, 4)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got, want := h.gitOutput("show", ":f.txt"), "1\nTWO\n3\nFOUR\n5\n"; got != want {
		t.Fatalf("index holds %q, want %q", got, want)
	}
	if got := h.gitOutput("diff", "--", "f.txt"); got != "" {
		t.Fatalf("worktree should have nothing left, got:\n%s", got)
	}
}

// TestDiscardWholeFile throws away everything unstaged in one operation and
// checks the file is byte-identical to the index afterwards.
func TestDiscardWholeFile(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nEXTRA\nthree\n")

	if _, err := DiscardLines("f.txt", worktreeDiff(t, "f.txt"), nil); err != nil {
		t.Fatalf("DiscardLines: %v", err)
	}
	if got, want := readFile(t, h), "one\ntwo\nthree\n"; got != want {
		t.Fatalf("worktree holds %q, want %q", got, want)
	}
	if got := h.gitOutput("diff", "--", "f.txt"); got != "" {
		t.Fatalf("worktree diff should be empty, got:\n%s", got)
	}
}

// TestDiscardRefusesConvertedFile: writing index-form bytes back to disk would
// change the file's line endings, so a file git converts is discarded whole.
func TestDiscardRefusesConvertedFile(t *testing.T) {
	h := newRepoHarness(t)
	h.write(".gitattributes", "*.txt text\n")
	h.write("f.txt", "one\ntwo\n")
	h.commitAll("base")
	h.write("f.txt", "one\r\nTWO\r\n")

	_, err := DiscardLines("f.txt", worktreeDiff(t, "f.txt"), nil)
	if !errors.Is(err, ErrRefusedPartial) {
		t.Fatalf("want ErrRefusedPartial, got %v", err)
	}
}

// TestStageNewFileLine stages one line of an untracked file, which git itself cannot
// do without intent-to-add.
func TestStageNewFileLine(t *testing.T) {
	h := newRepoHarness(t)
	h.write("seed.txt", "seed\n")
	h.commitAll("base")
	h.write("new.txt", "alpha\nbeta\ngamma\n")

	if _, err := StageLines("new.txt", lineSelection(0, 1)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got, want := h.gitOutput("show", ":new.txt"), "alpha\n"; got != want {
		t.Fatalf("index holds %q, want %q", got, want)
	}
	if got := h.gitOutput("diff", "--", "new.txt"); !strings.Contains(got, "+beta") || !strings.Contains(got, "+gamma") {
		t.Fatalf("worktree diff should still hold the rest:\n%s", got)
	}
}

// TestDiscardLine puts the worktree back the way the index has it, and the
// recorded undo restores the bytes exactly.
func TestDiscardLine(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nEXTRA\nthree\n")

	res, err := DiscardLines("f.txt", lineSelection(0, 3), nil)
	if err != nil {
		t.Fatalf("DiscardLines: %v", err)
	}
	// Only EXTRA was thrown away; the replacement above it stays.
	if got, want := readFile(t, h), "one\nTWO\nthree\n"; got != want {
		t.Fatalf("worktree holds %q, want %q", got, want)
	}

	if err := res.Undo.Apply(); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got, want := readFile(t, h), "one\nTWO\nEXTRA\nthree\n"; got != want {
		t.Fatalf("undo restored %q, want %q", got, want)
	}
}

func readFile(t *testing.T, h *repoHarness) string {
	t.Helper()
	// The discard tests all work on f.txt; a helper with a name nobody varies
	// would only add a parameter to keep in step.
	const name = "f.txt"
	b, err := os.ReadFile(filepath.Join(h.dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestStageRefusesFilteredFile covers the one transformation twig will not
// attempt: a clean filter's output is opaque, so the file is staged whole.
func TestStageRefusesFilteredFile(t *testing.T) {
	h := newRepoHarness(t)
	h.write(".gitattributes", "*.dat filter=demo\n")
	h.write("f.dat", "a\nb\n")
	h.commitAll("base")
	h.write("f.dat", "a\nB\n")

	_, err := StageLines("f.dat", lineSelection(2, 2))
	if !errors.Is(err, ErrRefusedPartial) {
		t.Fatalf("want ErrRefusedPartial, got %v", err)
	}
}

// TestStageConvertsLineEndings stages a line of a CRLF worktree file and
// checks the index holds LF, which is the form git stores.
func TestStageConvertsLineEndings(t *testing.T) {
	h := newRepoHarness(t)
	h.write(".gitattributes", "*.txt text\n")
	h.write("f.txt", "one\ntwo\n")
	h.commitAll("base")
	h.write("f.txt", "one\r\nTWO\r\nTHREE\r\n")

	if _, err := StageLines("f.txt", lineSelection(2, 2)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got, want := h.gitOutput("show", ":f.txt"), "one\nTWO\n"; got != want {
		t.Fatalf("index holds %q, want %q", got, want)
	}
}

// TestStageWholeHunk stages a hunk and leaves a later hunk alone.
func TestStageWholeHunk(t *testing.T) {
	h := newRepoHarness(t)
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		b.WriteString(string(rune('a' + i - 1)))
		b.WriteByte('\n')
	}
	h.write("f.txt", b.String())
	h.commitAll("base")

	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	lines[1] = "B"
	lines[10] = "K"
	h.write("f.txt", strings.Join(lines, "\n")+"\n")

	f, err := FileDiff("f.txt", false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	if f.NHunks() != 2 {
		t.Fatalf("%d hunks, want 2", f.NHunks())
	}
	if _, err := StageLines("f.txt", stage.HunkSelection(f, 0)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got := h.gitOutput("show", ":f.txt"); !strings.Contains(got, "\nB\n") || strings.Contains(got, "\nK\n") {
		t.Fatalf("staged the wrong hunk:\n%s", got)
	}
}

// TestDiscardRefusesAStalePreview: the dialog is a promise about which lines go.
// If the file changes while it is open, the same numbers name different text, so
// the discard refuses and leaves the worktree alone.
func TestDiscardRefusesAStalePreview(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nEXTRA\nthree\n")

	f, err := FileDiff("f.txt", false, Options{Context: 3})
	if err != nil {
		t.Fatalf("FileDiff: %v", err)
	}
	sel := lineSelection(0, 3)
	// The lines the dialog showed: every selected line, with its text.
	var shown []diff.Line
	for i := range f.Hunks {
		hunk, ok := f.HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range hunk.Lines {
			if key, ok := stage.LineOf(l); ok && sel.Has(key) {
				shown = append(shown, l)
			}
		}
	}
	if len(shown) == 0 {
		t.Fatal("the fixture selected nothing")
	}

	// The editor saves the very line the dialog was about, in the middle of the
	// dialog.
	h.write("f.txt", "one\nTWO\nEXTRA!\nthree\n")

	if _, err := DiscardLines("f.txt", sel, shown); !errors.Is(err, ErrStale) {
		t.Fatalf("discard err = %v, want ErrStale", err)
	}
	if got, want := readFile(t, h), "one\nTWO\nEXTRA!\nthree\n"; got != want {
		t.Fatalf("worktree holds %q, want it untouched at %q", got, want)
	}

	// The same preview against the unchanged file still goes through.
	h.write("f.txt", "one\nTWO\nEXTRA\nthree\n")
	if _, err := DiscardLines("f.txt", sel, shown); err != nil {
		t.Fatalf("discard on the previewed file: %v", err)
	}
	if got, want := readFile(t, h), "one\nTWO\nthree\n"; got != want {
		t.Fatalf("worktree holds %q, want %q", got, want)
	}
}

// TestDiscardRefusesAWriteRacedEdit covers the gap the preview check cannot:
// the state is read, then another process saves before the blend reaches the
// disk. Writing then would replace those newer bytes with a mix built from the
// older ones, so the write boundary re-reads the file.
func TestDiscardRefusesAWriteRacedEdit(t *testing.T) {
	h := newRepoHarness(t)
	h.write("f.txt", "one\ntwo\nthree\n")
	h.commitAll("base")
	h.write("f.txt", "one\nTWO\nthree\n")

	st, err := LoadStageState("f.txt")
	if err != nil {
		t.Fatalf("LoadStageState: %v", err)
	}
	if err := verifyWorktreeUnchanged(st); err != nil {
		t.Fatalf("an unchanged file was refused: %v", err)
	}

	// The save lands after the state was loaded and before the write.
	h.write("f.txt", "one\nTWO\nthree\nfour\n")
	if err := verifyWorktreeUnchanged(st); !errors.Is(err, ErrStale) {
		t.Fatalf("a raced write was not refused: %v", err)
	}

	// The same goes the other way: a file that was deleted and came back.
	h.write("gone.txt", "a\n")
	h.commitAll("base file")
	if err := os.Remove(filepath.Join(h.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	invalidate()
	gone, err := LoadStageState("gone.txt")
	if err != nil {
		t.Fatalf("LoadStageState: %v", err)
	}
	if err := verifyWorktreeUnchanged(gone); err != nil {
		t.Fatalf("a still deleted file was refused: %v", err)
	}
	h.write("gone.txt", "b\n")
	invalidate()
	if err := verifyWorktreeUnchanged(gone); !errors.Is(err, ErrStale) {
		t.Fatalf("a recreated file was not refused: %v", err)
	}
}
