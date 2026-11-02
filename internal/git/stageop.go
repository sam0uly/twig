package git

import (
	"errors"
	"fmt"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/stage"
)

// Undo is the exact reverse of one operation. It is a value, so the caller can
// keep a stack of them and replay them without asking git what changed.
type Undo struct {
	// Path the operation touched.
	Path string
	// Mode, OID and HadIndex restore the index record. HadIndex false means
	// the path was not in the index at all, so undoing removes the entry.
	Mode     string
	OID      string
	HadIndex bool
	// File, when non-nil, restores worktree bytes instead of an index record.
	File     []byte
	FileMode string
	// Label names the operation for the footer.
	Label string
}

// Apply reverses the operation.
func (u Undo) Apply() error {
	if u.File != nil {
		return WriteWorktreeContent(StageState{Path: u.Path, Mode: u.FileMode}, u.File)
	}
	if !u.HadIndex {
		return removeIndexPath(u.Path)
	}
	if err := setIndexBlob(indexRecord{Path: u.Path, Mode: u.Mode, OID: u.OID}, false); err != nil {
		return err
	}
	return nil
}

// StageResult reports one applied operation. Content is the file's new state
// on the side the operation wrote, so the caller can re-diff locally instead of
// asking git for the diff it just invalidated.
type StageResult struct {
	Undo    Undo
	State   StageState
	Content string
	OID     string
}

// diffFor builds the diff a blend needs. One line of context is required and
// enough: an addition carries no old side number of its own, so the context
// line before it is what anchors it in the preimage. More context changes
// nothing, which a test pins down. Whitespace tolerance is off for the same
// reason, only more so: a whitespace-only change is a change, not context.
func diffFor(path, oldText, newText string) *diff.File {
	kind := diff.Modified
	switch {
	case oldText == "" && newText != "":
		kind = diff.Added
	case newText == "" && oldText != "":
		kind = diff.Deleted
	}
	return diff.Generate(path, kind, oldText, newText, diff.GenerateOptions{Context: 1})
}

// refusal turns a blend refusal into an error the caller can show.
func refusal(path, reason string, err error) error {
	if errors.Is(err, stage.ErrTailOnly) {
		return fmt.Errorf("%w: %s: only the final newline changed", ErrRefusedPartial, path)
	}
	return fmt.Errorf("%w: %s: %s", ErrRefusedPartial, path, reason)
}

// complement is the selection of everything the user did not pick. Unstaging
// and discarding both work by moving a file to the *other* version, so the
// lines they write are precisely the ones left out of the selection.
func complement(f *diff.File, sel *stage.Selection) *stage.Selection {
	all := stage.AllSelection(f)
	all.Difference(sel)
	return all
}

// StageLines applies a selection from the worktree side to the index: selected
// additions enter it, selected deletions leave it, and every other line stays
// exactly as the index had it.
func StageLines(path string, sel *stage.Selection) (StageResult, error) {
	st, err := LoadStageState(path)
	if err != nil {
		return StageResult{}, err
	}
	if reason := st.PartialReason(); reason != "" {
		return StageResult{}, fmt.Errorf("%w: %s: %s", ErrRefusedPartial, path, reason)
	}

	f := diffFor(path, st.IndexText, st.WorkText)
	content, err := stage.Blend(st.IndexText, st.WorkText, f, sel)
	if err != nil {
		return StageResult{}, refusal(path, "cannot merge this selection", err)
	}

	oid, err := ApplyIndexContent(st, []byte(content))
	if err != nil {
		return StageResult{}, err
	}
	return StageResult{
		Undo: Undo{
			Path: path, Mode: st.Mode, OID: st.IndexOID,
			HadIndex: !isZeroOID(st.IndexOID), Label: "stage",
		},
		State:   st,
		Content: content,
		OID:     oid,
	}, nil
}

// UnstageLines takes a selection of the staged diff back out of the index. The
// content it writes is derived from the two blobs, so no worktree byte is ever
// involved.
func UnstageLines(path string, sel *stage.Selection) (StageResult, error) {
	st, err := LoadStageState(path)
	if err != nil {
		return StageResult{}, err
	}
	if st.StagedKind == 0 {
		return StageResult{}, fmt.Errorf("nothing staged for %q", path)
	}

	f := diffFor(path, st.HeadText, st.IndexText)
	content, err := stage.Blend(st.HeadText, st.IndexText, f, complement(f, sel))
	if err != nil {
		return StageResult{}, refusal(path, "cannot merge this selection", err)
	}

	// A path that HEAD does not have leaves the index entirely once nothing of
	// it is left; writing an empty blob would invent a file.
	var payload []byte
	if content == "" && isZeroOID(st.HeadOID) {
		payload = nil
	} else {
		payload = []byte(content)
	}

	oid, err := ApplyIndexContent(st, payload)
	if err != nil {
		return StageResult{}, err
	}
	return StageResult{
		Undo: Undo{
			Path: path, Mode: st.Mode, OID: st.IndexOID,
			HadIndex: !isZeroOID(st.IndexOID), Label: "unstage",
		},
		State:   st,
		Content: content,
		OID:     oid,
	}, nil
}

// DiscardLines puts the selected lines of the worktree back the way the index
// has them. It is the only operation that writes a file on disk, and its undo
// holds the exact bytes that were there before.
func DiscardLines(path string, sel *stage.Selection, shown []diff.Line) (StageResult, error) {
	st, err := LoadStageState(path)
	if err != nil {
		return StageResult{}, err
	}
	if reason := st.DiscardReason(); reason != "" {
		return StageResult{}, fmt.Errorf("%w: %s: %s", ErrRefusedPartial, path, reason)
	}
	if st.WorkKind == 0 {
		return StageResult{}, fmt.Errorf("no unstaged change for %q", path)
	}

	f := diffFor(path, st.IndexText, st.WorkText)
	// A discard is the one operation somebody reads a dialog before doing, so
	// the lines the dialog showed are part of the contract: if the file changed
	// underneath the dialog, the numbers no longer name the same text and the
	// honest answer is to refuse rather than to remove whatever they name now.
	if err := verifyPreview(f, shown); err != nil {
		return StageResult{}, err
	}
	content, err := stage.Blend(st.IndexText, st.WorkText, f, complement(f, sel))
	if err != nil {
		return StageResult{}, refusal(path, "cannot merge this selection", err)
	}
	if err := WriteWorktreeContent(st, []byte(content)); err != nil {
		return StageResult{}, err
	}
	return StageResult{
		Undo: Undo{
			Path: path, File: []byte(st.RawWorkText),
			FileMode: st.Mode, Label: "discard",
		},
		State:   st,
		Content: content,
	}, nil
}

// ErrStale is wrapped when the file changed between the preview and the write, so
// the lines the reader agreed to are no longer the lines the numbers name.
var ErrStale = errors.New("the file changed since the dialog opened")

// verifyPreview checks that every line the dialog showed still exists with the
// same text at the same side and number. It is a content check rather than a
// timestamp: two edits that leave the previewed lines where they were are fine,
// and the smallest change that moves them is not.
func verifyPreview(f *diff.File, shown []diff.Line) error {
	if len(shown) == 0 {
		return nil
	}
	now := make(map[stage.Line]string)
	for i := range f.Hunks {
		h, ok := f.HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			if key, ok := stage.LineOf(l); ok {
				now[key] = l.Text
			}
		}
	}
	for _, l := range shown {
		key, ok := stage.LineOf(l)
		if !ok {
			// A placeholder row ("… 12 more") names nothing to check.
			continue
		}
		if text, ok := now[key]; !ok || text != l.Text {
			return ErrStale
		}
	}
	return nil
}
