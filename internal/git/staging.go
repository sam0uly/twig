package git

import (
	"fmt"
	"os"
	"path/filepath"

	"samouly.fun/twig/internal/diff"
)

// StageState is everything a partial stage needs about one path: both sides'
// text, the mode the index record must repeat, and the ids that make an undo
// exact. The two sides are read once, so an operation cannot mix a stale half
// with a fresh one.
type StageState struct {
	// Path is relative to the work tree root.
	Path string
	// Mode is the mode the index record will carry.
	Mode string

	// IndexOID and IndexText are the index side. A path that is not in the
	// index has a zero id and no text.
	IndexOID  string
	IndexText string
	// HeadOID and HeadText are the HEAD side, which is the other half of an
	// unstage.
	HeadOID  string
	HeadText string
	// WorkText is the worktree side, already converted into the form the index
	// stores. It is empty for a file that was deleted.
	WorkText string
	// RawWorkText is the worktree side as it exists on disk, line endings and
	// all. An undo writes these bytes back, because a file that came from a
	// CRLF checkout must not return as LF.
	RawWorkText string

	// WorkKind is the unstaged change, StagedKind the staged one. Either can
	// be zero: a file may be changed on one side only.
	WorkKind   diff.ChangeKind
	StagedKind diff.ChangeKind

	// Untracked marks a path that is not in the index yet, Staged a path whose
	// index entry already differs from HEAD.
	Untracked bool
	Staged    bool

	// Filtered reports a clean/smudge filter, whose transformation twig does
	// not attempt. Text reports line ending normalisation, which twig does.
	Filtered bool
	Text     bool
}

// PartialReason is the reason this file cannot be staged a line at a time, or
// "" when it can. It is shown verbatim rather than turned into an error.
func (s StageState) PartialReason() string {
	switch {
	case s.Filtered:
		return "filtered file, staged whole"
	case s.WorkKind == diff.Renamed || s.WorkKind == diff.Copied,
		s.StagedKind == diff.Renamed || s.StagedKind == diff.Copied:
		return "rename, staged whole"
	case s.Mode == submoduleMode:
		return "submodule, staged whole"
	case s.Mode == "120000":
		return "symlink, staged whole"
	}
	return ""
}

// DiscardReason is the reason this file cannot have a line thrown away, or ""
// when it can. Line ending conversion is refused here and not for staging: the
// index always holds converted bytes, but writing converted bytes back to disk
// would change the file's format.
func (s StageState) DiscardReason() string {
	if reason := s.PartialReason(); reason != "" {
		return reason
	}
	if s.Text {
		return "line endings are converted, discarded whole"
	}
	return ""
}

// LoadStageState reads both sides of one path from the current snapshot.
func LoadStageState(path string) (StageState, error) {
	rc, err := open()
	if err != nil {
		return StageState{}, err
	}
	snap, err := rc.snapshotFor(false)
	if err != nil {
		return StageState{}, err
	}
	f, ok := snap.files[path]
	if !ok {
		return StageState{}, fmt.Errorf("%w for %q", ErrNoChange, path)
	}

	st := StageState{
		Path:       path,
		Mode:       f.indexMode,
		IndexOID:   f.indexOID,
		HeadOID:    f.headOID,
		WorkKind:   f.unstagedKind,
		StagedKind: f.stagedKind,
		Untracked:  f.untracked,
		Staged:     f.staged,
	}
	if st.IndexText, err = rc.blob(f.indexOID); err != nil {
		return StageState{}, err
	}
	if st.HeadText, err = rc.blob(f.headOID); err != nil {
		return StageState{}, err
	}

	if st.Mode == "" {
		st.Mode = worktreeMode(rc.root, path)
	}
	if st.WorkKind != diff.Deleted && !isSymlinkMode(st.Mode) {
		if st.RawWorkText, err = rc.worktreeContent(path); err != nil {
			return StageState{}, err
		}
	}

	attrs, err := rc.attributesFor(path)
	if err != nil {
		return StageState{}, err
	}
	st.Filtered, st.Text = attrs.Filtered, attrs.Text
	st.WorkText = st.RawWorkText
	if st.Text {
		st.WorkText = toIndexForm(st.WorkText)
	}
	return st, nil
}

// worktreeMode derives the mode a new file should enter the index with: git
// distinguishes plain files, executables and symlinks, and nothing else.
func worktreeMode(root, path string) string {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return "100644"
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "120000"
	case info.Mode()&0o111 != 0:
		return "100755"
	}
	return "100644"
}

func isSymlinkMode(mode string) bool { return mode == "120000" }

// ApplyIndexContent writes new content as the path's index record and returns
// the object id it stored. A nil content removes the record entirely, which is
// how unstaging a file that only exists in the index is expressed.
func ApplyIndexContent(st StageState, content []byte) (string, error) {
	if content == nil {
		if err := removeIndexPath(st.Path); err != nil {
			return "", err
		}
		return "", nil
	}
	rc, err := open()
	if err != nil {
		return "", err
	}
	oid, err := rc.writeBlob(content)
	if err != nil {
		return "", err
	}
	add := st.Untracked || isZeroOID(st.IndexOID)
	if err := setIndexBlob(indexRecord{Path: st.Path, Mode: st.Mode, OID: oid}, add); err != nil {
		return "", err
	}
	return oid, nil
}

// WriteWorktreeContent replaces a file on disk. A filtered file is handed back
// to git: only git can run the smudge filter that produces the bytes the user
// expects to see.
func WriteWorktreeContent(st StageState, content []byte) error {
	rc, err := open()
	if err != nil {
		return err
	}
	if st.Filtered {
		if _, _, err := runGitWrite(rc.root, nil, "restore", "--worktree", "--", st.Path); err != nil {
			return err
		}
		invalidate()
		return nil
	}

	full := filepath.Join(rc.root, filepath.FromSlash(st.Path))
	mode := os.FileMode(0o644)
	if st.Mode == "100755" {
		mode = 0o755
	}
	if err := os.WriteFile(full, content, mode); err != nil {
		return err
	}
	// WriteFile only applies the mode when it creates the file, so set it
	// explicitly for a file that already existed with another one.
	if err := os.Chmod(full, mode); err != nil {
		return err
	}
	invalidate()
	return nil
}
