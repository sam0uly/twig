// Package git reads repository state and performs staging operations by
// driving the git binary through its machine-readable porcelain. Line and
// word-level differences are produced by twig's own diff engine
// (internal/diff.Generate), which supports context lines and
// whitespace-ignoring comparison natively.
package git

import (
	"errors"
	"fmt"
	"strings"

	"samouly.fun/twig/internal/diff"
)

// ErrNotARepository is returned when the current directory is not inside a
// git work tree.
var ErrNotARepository = errors.New("not inside a git repository")

// ErrUnknownRef is returned when a ref names nothing in this repository. It is
// a mistyped argument rather than a repository failure, so callers can say that
// instead of quoting a git command line.
var ErrUnknownRef = errors.New("unknown ref")

// SourceKind identifies which changes are being viewed.
type SourceKind int

// Possible source kinds.
const (
	// SourceWorktree shows unstaged changes in the working tree.
	SourceWorktree SourceKind = iota
	// SourceStaged shows changes staged for commit.
	SourceStaged
	// SourceCommit shows the changes a single commit introduced.
	SourceCommit
	// SourceRange shows the changes between two refs.
	SourceRange
)

// Source describes which diff the viewer should show.
type Source struct {
	Ref1 string
	Ref2 string
	// Paths limits the view to these paths, the way git's pathspec does: a path
	// selects itself, a directory selects everything under it. Empty means all.
	Paths []string
	Kind  SourceKind
}

// Matches reports whether a path belongs to the source's pathspec.
func (s Source) Matches(path string) bool {
	if len(s.Paths) == 0 {
		return true
	}
	for _, want := range s.Paths {
		want = strings.TrimSuffix(want, "/")
		if want == "" || path == want || strings.HasPrefix(path, want+"/") {
			return true
		}
	}
	return false
}

// Filter narrows a file list to the source's pathspec.
func (s Source) Filter(files []*diff.File) []*diff.File {
	if len(s.Paths) == 0 {
		return files
	}
	kept := files[:0]
	for _, f := range files {
		if s.Matches(f.Path()) {
			kept = append(kept, f)
		}
	}
	return kept
}

// Describe returns a short human readable description of the source.
func (s Source) Describe() string {
	switch s.Kind {
	case SourceStaged:
		return "staged"
	case SourceCommit:
		return s.Ref1
	case SourceRange:
		return s.Ref1 + ".." + s.Ref2
	default:
		return "worktree"
	}
}

// Pinned reports whether the source is a fixed diff rather than the live
// working tree. A commit or a range has nothing to write back to and nothing to
// refresh against, so the viewer treats it as a document to read.
func (s Source) Pinned() bool {
	return s.Kind == SourceCommit || s.Kind == SourceRange
}

// StagedOnly reports whether only the index side of the working tree is in
// play, which is what --staged asks for.
func (s Source) StagedOnly() bool {
	return s.Kind == SourceStaged
}

// Options controls how the diff is produced.
type Options struct {
	// Context is the number of context lines shown around hunks.
	Context int
	// IgnoreWhitespace ignores whitespace changes when comparing lines.
	IgnoreWhitespace bool
}

// Commit is a single entry in the repository log.
type Commit struct {
	Hash    string
	Short   string
	Author  string
	When    string
	Subject string
	// Description is the first line of the commit body, empty when the
	// commit has no body.
	Description string
}

// CheckRepository verifies the current directory is inside a git work tree.
func CheckRepository() error {
	if _, err := open(); err != nil {
		return ErrNotARepository
	}
	return nil
}

// Diff returns the parsed diff for the given source.
func Diff(src Source, opts Options) (*diff.Diff, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}

	var files []*diff.File
	switch src.Kind {
	case SourceStaged:
		// A whole-document request is expected to be current, so refresh the
		// snapshot instead of reusing whatever the last status left behind.
		if _, err := rc.snapshotFor(true); err != nil {
			return nil, err
		}
		files, err = rc.stagedFiles(opts)
	case SourceCommit:
		files, err = rc.commitFiles(src.Ref1, opts)
	case SourceRange:
		files, err = rc.rangeFiles(src.Ref1, src.Ref2, opts)
	default:
		if _, err := rc.snapshotFor(true); err != nil {
			return nil, err
		}
		files, err = rc.worktreeFiles(opts)
	}
	if err != nil {
		return nil, err
	}
	files = src.Filter(files)

	d := &diff.Diff{Files: files}
	for _, f := range files {
		d.Additions += f.Additions
		d.Deletions += f.Deletions
	}
	return d, nil
}

// commitFiles shows the changes a single commit introduced, relative to its
// first parent (or an empty tree for root commits).
func (rc *repoContext) commitFiles(ref string, opts Options) ([]*diff.File, error) {
	out, err := rc.diffTreeOut(ref)
	if err != nil {
		return nil, err
	}
	return rc.patchFiles(out, opts)
}

// rangeFiles shows the changes between two refs.
func (rc *repoContext) rangeFiles(fromRef, toRef string, opts Options) ([]*diff.File, error) {
	out, err := rc.git("diff-tree", "-r", "-M", "-z", fromRef, toRef)
	if err != nil {
		return nil, fmt.Errorf("diff %q..%q: %w", fromRef, toRef, err)
	}
	return rc.patchFiles(out, opts)
}

// RefNames lists the refs a diff can name: HEAD, local branches, tags and
// remote branches, in the order git prints them. It is what shell completion
// offers for the positional argument.
func RefNames() ([]string, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	out, err := rc.git("for-each-ref", "--format=%(refname:short)",
		"refs/heads", "refs/tags", "refs/remotes")
	if err != nil {
		return nil, err
	}

	names := []string{"HEAD"}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// Log returns the most recent commits, most recent first.
func Log(limit int) ([]Commit, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}

	out, err := rc.git("log", "-n", fmt.Sprint(limit),
		"--pretty=%H%x1f%h%x1f%an%x1f%ad%x1f%s%x1f%b%x1e", "--date=format:%b %d, %y")
	if err != nil {
		// An unborn branch has no log but is not an error to browse.
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, fmt.Errorf("read log: %w", err)
	}

	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}
		c := Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			When:    fields[3],
			Subject: fields[4],
		}
		if len(fields) > 5 {
			// The whole body travels: the viewer shows it in full, line for
			// line, under the subject.
			c.Description = strings.TrimSpace(fields[5])
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// firstLine returns the first non-empty line of s, trimmed.

// Branch returns the current branch name, or "HEAD" when detached.
func Branch() string {
	rc, err := open()
	if err != nil {
		return ""
	}
	name, err := rc.git("branch", "--show-current")
	if err != nil {
		return "HEAD"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "HEAD"
	}
	return name
}

// Stage stages the given paths into the index.
func Stage(paths ...string) error {
	if len(paths) == 0 {
		return errors.New("no paths to stage")
	}
	rc, err := open()
	if err != nil {
		return err
	}
	for _, p := range paths {
		if _, err := rc.git("add", "--", p); err != nil {
			return fmt.Errorf("stage %q: %w", p, err)
		}
	}
	invalidate()
	return nil
}

// Unstage removes the given paths from the index without touching the
// working tree.
func Unstage(paths ...string) error {
	if len(paths) == 0 {
		return errors.New("no paths to unstage")
	}
	rc, err := open()
	if err != nil {
		return err
	}

	for _, p := range paths {
		if _, err := rc.git("reset", "-q", "HEAD", "--", p); err != nil {
			// An unborn branch has no HEAD to reset to: dropping the index
			// entry is the same operation for git there.
			if _, err2 := rc.git("rm", "--cached", "-q", "--", p); err2 != nil {
				return fmt.Errorf("unstage %q: %w", p, err)
			}
		}
	}
	invalidate()
	return nil
}

// sortFiles orders files by path for a stable listing.
func sortFiles(files []*diff.File) {
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && files[j].Path() < files[j-1].Path(); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

func generateOptions(opts Options) diff.GenerateOptions {
	return diff.GenerateOptions{
		Context:          opts.Context,
		IgnoreWhitespace: opts.IgnoreWhitespace,
	}
}
