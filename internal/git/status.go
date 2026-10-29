package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"samouly.fun/twig/internal/diff"
)

// FileStatus is one changed file as shown in the Files page.
type FileStatus struct {
	Path string
	// OldPath is set for renames: the path the file had before.
	OldPath string
	// Letter is the one-letter status code, e.g. "M", "A", "D", "?".
	Letter string
	// Staged reports whether the change sits in the index.
	Staged bool
	// Untracked marks files that are not in the index at all.
	Untracked bool
}

// DisplayPath is the path as listed; renames read "old → new" like git's own
// output. Path keeps the current name so lookups stay exact.
func (f FileStatus) DisplayPath() string {
	if f.OldPath != "" {
		return f.OldPath + " → " + f.Path
	}
	return f.Path
}

// Status returns the changed files of the working tree, unstaged files
// first-class with untracked files included, plus the staged set. It forces a
// fresh snapshot: everything else in the session reuses that one result.
func Status() ([]FileStatus, []FileStatus, error) {
	rc, err := open()
	if err != nil {
		return nil, nil, err
	}
	if rc.worktree == nil {
		return nil, nil, ErrNotARepository
	}

	snap, err := rc.snapshotFor(true)
	if err != nil {
		return nil, nil, err
	}
	return snap.staged, snap.unstaged, nil
}

func sortStatus(s []FileStatus) {
	sort.Slice(s, func(i, j int) bool { return s[i].Path < s[j].Path })
}

// ProjectName returns the repository name: the base name of the work tree
// root, or the remote repo name when available.
func ProjectName() string {
	rc, err := open()
	if err != nil || rc.worktree == nil {
		return ""
	}
	root := rc.worktree.Filesystem.Root()
	name := filepath.Base(strings.TrimSuffix(root, "/"))
	if name == "" || name == "/" || name == "." {
		return ""
	}
	return name
}

// DiffTarget identifies one file in the all-changes view.
type DiffTarget struct {
	Path   string
	Staged bool
}

// DiffTargets diffs each target in the given order. Every target yields one
// File, even when its diff cannot be produced, so sidebar positions always
// map onto diff bands.
func DiffTargets(targets []DiffTarget, opts Options) ([]*diff.File, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	if rc.worktree == nil {
		return nil, ErrNotARepository
	}

	snap, err := rc.snapshotFor(false)
	if err != nil {
		return nil, err
	}

	files := make([]*diff.File, 0, len(targets))
	for _, t := range targets {
		var f *diff.File
		if sf, ok := snap.fileFor(t.Path, t.Staged); ok {
			f, _ = rc.diffFile(sf, t.Staged, opts)
		}
		if f == nil {
			f = unavailable(t.Path)
		}
		files = append(files, f)
	}
	return files, nil
}

// unavailable stands in for a file whose diff could not be produced, so the
// sidebar keeps its one-to-one mapping onto diff bands.
func unavailable(path string) *diff.File {
	return diff.Unavailable(path, "")
}

// unavailableBlob is the stand-in for a file whose content is not in the local
// object database: a shallow or partial (blobless) clone, or a submodule whose
// repository is not checked out. One path must not take the whole view down.
func unavailableBlob(path string, err error) *diff.File {
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return diff.Unavailable(path, "content not present locally")
	}
	return diff.Unavailable(path, err.Error())
}

// CommitChange is one path a commit changed, derived from tree data alone: no
// blob is read, so listing even a merge commit is cheap and works in a partial
// clone. The content is loaded per file, when it is actually looked at.
type CommitChange struct {
	Path      string
	OldPath   string
	Kind      diff.ChangeKind
	Submodule bool
}

// CommitChangeList lists the paths a commit changed, relative to its first
// parent.
func CommitChangeList(ref string) ([]CommitChange, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	commit, from, to, err := rc.commitTrees(ref)
	if err != nil {
		return nil, err
	}
	_ = commit

	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}

	out := make([]CommitChange, 0, len(changes))
	for _, c := range changes {
		path, oldPath := c.To.Name, c.From.Name
		if path == "" {
			path = oldPath
		}
		if path == "" {
			continue
		}
		kind := diff.Modified
		switch {
		case c.From.TreeEntry.Hash.IsZero():
			kind = diff.Added
		case c.To.TreeEntry.Hash.IsZero():
			kind = diff.Deleted
		case oldPath != "" && path != "" && oldPath != path:
			kind = diff.Renamed
		}
		out = append(out, CommitChange{
			Path:      path,
			OldPath:   oldPath,
			Kind:      kind,
			Submodule: submoduleMode(c.From.TreeEntry.Mode) || submoduleMode(c.To.TreeEntry.Mode),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// FileAtCommit builds one file's diff for a commit, on demand.
func FileAtCommit(ref string, c CommitChange, opts Options) (*diff.File, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	if c.Submodule {
		return diff.Unavailable(c.Path, "submodule"), nil
	}

	_, from, to, err := rc.commitTrees(ref)
	if err != nil {
		return nil, err
	}

	oldPath := c.OldPath
	if oldPath == "" {
		oldPath = c.Path
	}
	oldText, newText := "", ""
	if fromEntry, err := from.File(oldPath); err == nil {
		if oldText, err = rc.blobContent(fromEntry.Hash); err != nil {
			return unavailableBlob(c.Path, err), nil
		}
	}
	if toEntry, err := to.File(c.Path); err == nil {
		if newText, err = rc.blobContent(toEntry.Hash); err != nil {
			return unavailableBlob(c.Path, err), nil
		}
	}

	f := diff.Generate(c.Path, c.Kind, oldText, newText, generateOptions(opts))
	f.OldPath = oldPath
	f.NewPath = c.Path
	return f, nil
}

// commitTrees resolves a commit and its parent's tree (the empty tree for a root
// commit).
func (rc *repoContext) commitTrees(ref string) (*object.Commit, *object.Tree, *object.Tree, error) {
	commit, err := rc.resolveCommit(ref)
	if err != nil {
		return nil, nil, nil, err
	}
	toTree, err := commit.Tree()
	if err != nil {
		return nil, nil, nil, err
	}
	fromTree := &object.Tree{}
	if commit.NumParents() > 0 {
		parent, err := commit.Parents().Next()
		if err != nil {
			return nil, nil, nil, err
		}
		if fromTree, err = parent.Tree(); err != nil {
			return nil, nil, nil, err
		}
	}
	return commit, fromTree, toTree, nil
}

// submoduleMode reports whether a tree entry is a gitlink.
func submoduleMode(m filemode.FileMode) bool { return m == filemode.Submodule }

// FileDiff generates the diff of a single file: staged compares HEAD with
// the index; unstaged compares the index with the working tree (untracked
// files diff against an empty old side).
func FileDiff(path string, staged bool, opts Options) (*diff.File, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	if rc.worktree == nil {
		return nil, ErrNotARepository
	}
	return rc.fileDiff(path, staged, opts)
}

// fileDiff answers from the snapshot: no repository open, no index decode and
// no worktree walk, just the cached contents of the two sides.
func (rc *repoContext) fileDiff(path string, staged bool, opts Options) (*diff.File, error) {
	snap, err := rc.snapshotFor(false)
	if err != nil {
		return nil, err
	}
	f, ok := snap.fileFor(path, staged)
	if !ok {
		return nil, fmt.Errorf("no change for %q", path)
	}
	return rc.diffFile(f, staged, opts)
}

// CommitMessageTooShort is returned when a commit message is empty.
var CommitMessageTooShort = errors.New("commit message is empty")

// CommitChanges commits the staged changes with the given message and
// returns the new commit hash.
func CommitChanges(message string) (string, error) {
	if message == "" {
		return "", CommitMessageTooShort
	}
	rc, err := open()
	if err != nil {
		return "", err
	}
	if rc.worktree == nil {
		return "", ErrNotARepository
	}

	opts := &git.CommitOptions{}
	if rc.head == nil {
		// First commit on an unborn branch.
		opts = &git.CommitOptions{}
	}

	c, err := rc.worktree.Commit(message, opts)
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	invalidate()
	return c.String(), nil
}

// Push pushes the current branch to its upstream. Authentication is
// attempted through the SSH agent and then anonymously.
func Push() error {
	rc, err := open()
	if err != nil {
		return err
	}

	ref, err := rc.repo.Head()
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	_ = ref

	if err := rc.repo.Push(&git.PushOptions{}); err == nil {
		return nil
	}

	if auth, err := ssh.NewSSHAgentAuth("git"); err == nil {
		if err := rc.repo.Push(&git.PushOptions{Auth: auth}); err == nil {
			return nil
		}
	}

	// Re-push with no auth to surface the real error message.
	if err := rc.repo.Push(&git.PushOptions{}); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}
