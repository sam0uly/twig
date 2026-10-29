package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

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
// root.
func ProjectName() string {
	rc, err := open()
	if err != nil {
		return ""
	}
	name := filepath.Base(strings.TrimSuffix(rc.root, "/"))
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

// diffTreeOut runs diff-tree for one commit against its first parent (the
// empty tree for a root commit).
func (rc *repoContext) diffTreeOut(ref string) (string, error) {
	parent, err := rc.git("rev-parse", "--verify", "--quiet", ref+"^")
	if err != nil || strings.TrimSpace(parent) == "" {
		out, err := rc.git("diff-tree", "-r", "-M", "-z", "--root", ref)
		if err != nil {
			return "", fmt.Errorf("diff %q: %w", ref, err)
		}
		return out, nil
	}
	out, err := rc.git("diff-tree", "-r", "-M", "-z", strings.TrimSpace(parent), ref)
	if err != nil {
		return "", fmt.Errorf("diff %q: %w", ref, err)
	}
	return out, nil
}

// CommitChange is one path a commit changed, derived from tree data alone:
// no blob is read, so listing even a merge commit is cheap and works in a
// partial clone. The content is loaded per file, when it is actually looked
// at.
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

	out, err := rc.diffTreeOut(ref)
	if err != nil {
		return nil, err
	}

	changes := parseRaw(out)
	list := make([]CommitChange, 0, len(changes))
	for _, c := range changes {
		path, oldPath := c.path, c.oldPath
		if path == "" {
			path = oldPath
		}
		if path == "" {
			continue
		}
		kind := diff.Modified
		switch {
		case isZeroOID(c.fromOID):
			kind = diff.Added
		case isZeroOID(c.toOID):
			kind = diff.Deleted
		case oldPath != "" && path != "" && oldPath != path:
			kind = diff.Renamed
		}
		list = append(list, CommitChange{
			Path:      path,
			OldPath:   oldPath,
			Kind:      kind,
			Submodule: c.fromMode == submoduleMode || c.toMode == submoduleMode,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list, nil
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

	oldPath := c.OldPath
	if oldPath == "" {
		oldPath = c.Path
	}

	// The same diff-tree run lists the two blob ids the content is read
	// from; re-running it for one file is cheap next to the content reads.
	out, err := rc.diffTreeOut(ref)
	if err != nil {
		return nil, err
	}
	fromOID, toOID := rawOIDsFor(out, c.Path, c.OldPath)

	oldText, err := rc.blob(fromOID)
	if err != nil {
		return unavailableBlob(c.Path, err), nil
	}
	newText, err := rc.blob(toOID)
	if err != nil {
		return unavailableBlob(c.Path, err), nil
	}

	f := diff.Generate(c.Path, c.Kind, oldText, newText, generateOptions(opts))
	f.OldPath = oldPath
	f.NewPath = c.Path
	return f, nil
}

// rawOIDsFor finds the from/to blob ids of one path in diff-tree output,
// matching either the new path or, for renames, the old one.
func rawOIDsFor(out, path, oldPath string) (string, string) {
	for _, c := range parseRaw(out) {
		if c.path == path || (oldPath != "" && c.oldPath == oldPath) {
			return c.fromOID, c.toOID
		}
	}
	return "", ""
}

// FileDiff generates the diff of a single file: staged compares HEAD with
// the index; unstaged compares the index with the working tree (untracked
// files diff against an empty old side).
func FileDiff(path string, staged bool, opts Options) (*diff.File, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	return rc.fileDiff(path, staged, opts)
}

// fileDiff answers from the snapshot: no extra status run and no repository
// open, just the cached contents of the two sides.
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

	if _, err := rc.git("commit", "-q", "-m", message); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}

	hash, err := rc.git("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	invalidate()
	return strings.TrimSpace(hash), nil
}

// Push pushes the current branch to its upstream. Authentication goes
// through whatever git itself is configured to use.
func Push() error {
	rc, err := open()
	if err != nil {
		return err
	}

	if _, err := rc.git("push"); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}
