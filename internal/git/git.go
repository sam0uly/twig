// Package git integrates with go-git: it reads repository state (status,
// diffs, log) and performs staging operations without shelling out to the
// git binary. Line and word-level differences are produced by twig's own
// diff engine (internal/diff.Generate), which supports context lines and
// whitespace-ignoring comparison natively.
package git

import (
	"errors"
	"fmt"
	"io"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"samouly.fun/twig/internal/diff"
)

// ErrNotARepository is returned when the current directory is not inside a
// git work tree.
var ErrNotARepository = errors.New("not inside a git repository")

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
	Kind SourceKind
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
	commit, err := rc.resolveCommit(ref)
	if err != nil {
		return nil, err
	}

	toTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	fromTree := &object.Tree{}
	if commit.NumParents() > 0 {
		parent, err := commit.Parents().Next()
		if err != nil {
			return nil, err
		}
		if fromTree, err = parent.Tree(); err != nil {
			return nil, err
		}
	}

	return rc.treeDiff(fromTree, toTree, opts)
}

// rangeFiles shows the changes between two refs.
func (rc *repoContext) rangeFiles(fromRef, toRef string, opts Options) ([]*diff.File, error) {
	from, err := rc.treeForRef(fromRef)
	if err != nil {
		return nil, err
	}
	to, err := rc.treeForRef(toRef)
	if err != nil {
		return nil, err
	}
	return rc.treeDiff(from, to, opts)
}

// treeForRef resolves a ref, branch or commit hash to its tree.
func (rc *repoContext) treeForRef(ref string) (*object.Tree, error) {
	commit, err := rc.resolveCommit(ref)
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

func (rc *repoContext) resolveCommit(ref string) (*object.Commit, error) {
	if ref == "" || ref == "HEAD" {
		// HEAD moves when the repository is used outside twig.
		rc.refreshHead()
		if rc.head == nil {
			return nil, errors.New("unborn HEAD")
		}
		return rc.head, nil
	}

	h, err := rc.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", ref, err)
	}
	return rc.repo.CommitObject(*h)
}

// treeDiff generates a file diff for every change between two trees. A file
// whose content is missing locally (a shallow or blobless partial clone, or a
// submodule) becomes a one-line band instead of failing the whole view: the
// listing is still true, and the reason is visible where the diff would be.
func (rc *repoContext) treeDiff(from, to *object.Tree, opts Options) ([]*diff.File, error) {
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}

	var files []*diff.File
	for _, c := range changes {
		fromName, toName := c.From.Name, c.To.Name

		path := toName
		if path == "" {
			path = fromName
		}

		if submoduleMode(c.From.TreeEntry.Mode) || submoduleMode(c.To.TreeEntry.Mode) {
			files = append(files, diff.Unavailable(path, "submodule"))
			continue
		}

		oldText, err := rc.blobContent(c.From.TreeEntry.Hash)
		if err != nil {
			files = append(files, unavailableBlob(path, err))
			continue
		}
		newText, err := rc.blobContent(c.To.TreeEntry.Hash)
		if err != nil {
			files = append(files, unavailableBlob(path, err))
			continue
		}

		kind := diff.Modified
		switch {
		case c.From.TreeEntry.Hash.IsZero():
			kind = diff.Added
		case c.To.TreeEntry.Hash.IsZero():
			kind = diff.Deleted
		case fromName != toName && fromName != "" && toName != "":
			kind = diff.Renamed
		}

		f := diff.Generate(path, kind, oldText, newText, generateOptions(opts))
		f.OldPath = fromName
		f.NewPath = toName
		files = append(files, f)
	}

	sortFiles(files)
	return files, nil
}

// Log returns the most recent commits, most recent first.
func Log(limit int) ([]Commit, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	rc.refreshHead()
	if rc.head == nil {
		return nil, nil
	}

	iter, err := rc.repo.Log(&git.LogOptions{From: rc.head.Hash})
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	defer iter.Close()

	var commits []Commit
	for range limit {
		c, err := iter.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		commits = append(commits, Commit{
			Hash:    c.Hash.String(),
			Short:   c.Hash.String()[:7],
			Author:  c.Author.Name,
			When:    c.Author.When.Format("Jan 2, 2006"),
			Subject: firstLine(c.Message),
		})
	}
	return commits, nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

// Branch returns the current branch name, or "HEAD" when detached.
func Branch() string {
	rc, err := open()
	if err != nil {
		return ""
	}
	ref, err := rc.repo.Head()
	if err != nil {
		return "HEAD"
	}
	if ref.Name().IsBranch() {
		return ref.Name().Short()
	}
	return "HEAD"
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
	if rc.worktree == nil {
		return ErrNotARepository
	}
	for _, p := range paths {
		if err := rc.worktree.AddWithOptions(&git.AddOptions{Path: p}); err != nil {
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
	if rc.worktree == nil {
		return ErrNotARepository
	}

	headHash := plumbing.ZeroHash
	if rc.head != nil {
		headHash = rc.head.Hash
	}
	if err := rc.worktree.Reset(&git.ResetOptions{
		Mode:   git.MixedReset,
		Commit: headHash,
		Files:  paths,
	}); err != nil {
		return fmt.Errorf("unstage: %w", err)
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
