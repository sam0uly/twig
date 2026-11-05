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
	// Size and ModTime are the worktree file's stat, taken by the same Lstat
	// that fingerprints the snapshot. ModTime zero means unknown, which is the
	// case for a row that only has an index side.
	Size    int64
	ModTime int64
}

// DisplayPath is the path as listed; renames read "old → new" like git's own
// output. Path keeps the current name so lookups stay exact.
func (f FileStatus) DisplayPath() string {
	if f.OldPath != "" {
		return f.OldPath + " → " + f.Path
	}
	return f.Path
}

// Status is one coherent view of the working tree.
type Status struct {
	// Staged and Unstaged are the two columns of the files page, unstaged
	// first-class with untracked files included.
	Staged   []FileStatus
	Unstaged []FileStatus
	// Fingerprint identifies the whole view: the index and HEAD stamps plus
	// the stat signature of every changed path. Two refreshes that produce
	// the same fingerprint found nothing new, so the second can be dropped
	// without repainting or reloading anything.
	Fingerprint string
}

// WorkingTree returns the changed files of the working tree. It forces a fresh
// snapshot: everything else in the session reuses that one result.
func WorkingTree() (Status, error) {
	rc, err := open()
	if err != nil {
		return Status{}, err
	}

	snap, err := rc.snapshotFor(true)
	if err != nil {
		return Status{}, err
	}
	return Status{
		Staged:      snap.staged,
		Unstaged:    snap.unstaged,
		Fingerprint: snap.fp,
	}, nil
}

func sortStatus(s []FileStatus) {
	sort.Slice(s, func(i, j int) bool { return s[i].Path < s[j].Path })
}

// WorktreePath resolves a repository-relative path to one the filesystem can
// open. git reports paths relative to the work tree root, which is not where the
// process started when twig runs in a subdirectory.
func WorktreePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	rc, err := open()
	if err != nil {
		return path
	}
	return filepath.Join(rc.root, filepath.FromSlash(path))
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

	files := make([]*diff.File, len(targets))
	// Reading both sides is a stat and a read per file, done here so nothing
	// touches the repository off this goroutine. The diffs themselves are the
	// expensive part and are independent, so they are built on the cores.
	jobs := make([]diffJob, len(targets))
	for i, t := range targets {
		sf, ok := snap.fileFor(t.Path, t.Staged)
		if !ok {
			files[i] = unavailable(t.Path)
			continue
		}
		if jobs[i], err = rc.prepareDiff(sf, t.Staged, opts); err != nil {
			files[i] = unavailable(t.Path)
		}
	}
	for i, f := range rc.buildJobs(jobs) {
		if files[i] == nil {
			files[i] = f
		}
		if files[i] == nil {
			files[i] = unavailable(targets[i].Path)
		}
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
		return nil, fmt.Errorf("%w for %q", ErrNoChange, path)
	}
	return rc.diffFile(f, staged, opts)
}

// ErrCommitMessageTooShort is returned when a commit message is empty.
var ErrCommitMessageTooShort = errors.New("commit message is empty")

// CommitOptions tunes how a commit is made.
type CommitOptions struct {
	// Amend replaces the previous commit instead of adding one.
	Amend bool
}

// CommitChanges commits the staged changes with the given message and
// returns the new commit hash.
func CommitChanges(message string) (string, error) {
	return CommitWith(message, CommitOptions{})
}

// CommitWith commits the staged changes, optionally amending the previous
// commit. The message travels as an argument, so git's own cleanup rules apply
// and the user's editor is never involved: the message is the one in twig's
// box, and nothing rewrites it behind the reader's back.
func CommitWith(message string, opts CommitOptions) (string, error) {
	if message == "" {
		return "", ErrCommitMessageTooShort
	}
	rc, err := open()
	if err != nil {
		return "", err
	}

	args := []string{"commit", "-q", "-m", message}
	if opts.Amend {
		args = append(args, "--amend")
	}
	if _, _, err := runGitWrite(rc.root, nil, args...); err != nil {
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
