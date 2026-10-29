package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"

	"samouly.fun/twig/internal/diff"
)

// snapshot is one coherent view of the repository state: the staged changes
// (HEAD vs index, pure object comparison) and the worktree changes (index vs
// disk, one stat-first walk). Everything else reads from it, so no operation
// re-runs a full status.
type snapshot struct {
	staged   []FileStatus
	unstaged []FileStatus
	files    map[string]*snapFile
}

// snapFile is what a diff needs to know about one path.
type snapFile struct {
	path      string
	oldPath   string
	staged    bool
	untracked bool

	stagedKind   diff.ChangeKind
	unstagedKind diff.ChangeKind

	// a file whose mode changed but whose content did not still counts as a
	// change, exactly like git's "old mode / new mode" output.
	modeChanged bool

	headHash  plumbing.Hash
	indexHash plumbing.Hash
	workHash  plumbing.Hash
}

// snapshotFor returns a snapshot, computing it when missing (or always when
// force is set, which is what a refresh does). Readers that only need the
// current state (a block being re-materialised while painting) take the cached
// pointer without waiting for a walk that is already in flight.
func (rc *repoContext) snapshotFor(force bool) (*snapshot, error) {
	if !force {
		if snap := rc.snapshot.Load(); snap != nil {
			return snap, nil
		}
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !force {
		if snap := rc.snapshot.Load(); snap != nil {
			return snap, nil
		}
	}

	snap, err := rc.buildSnapshot()
	if err != nil {
		return nil, err
	}
	rc.snapshot.Store(snap)
	return snap, nil
}

// buildSnapshot computes the staged and worktree halves of the status. The
// caller holds rc.mu.
func (rc *repoContext) buildSnapshot() (*snapshot, error) {
	if rc.worktree == nil {
		return nil, ErrNotARepository
	}

	// The repository moves underneath us: commits, checkouts and resets run
	// outside twig all the time.
	rc.refreshHeadLocked()

	idx, idxMod, err := rc.indexLocked()
	if err != nil {
		return nil, err
	}
	headTree, err := rc.headTreeLocked()
	if err != nil {
		return nil, err
	}
	headMap, err := treeMap(headTree)
	if err != nil {
		return nil, err
	}

	indexMap := make(map[string]*index.Entry, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Stage != 0 {
			continue
		}
		indexMap[e.Name] = e
	}

	snap := &snapshot{files: make(map[string]*snapFile)}

	// ---- staged: HEAD tree vs index, no filesystem access.
	paths := make([]string, 0, len(indexMap)+len(headMap))
	for p := range indexMap {
		paths = append(paths, p)
	}
	for p := range headMap {
		if _, ok := indexMap[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var added, deleted []string
	for _, p := range paths {
		ie, inIndex := indexMap[p]
		he, inHead := headMap[p]

		switch {
		case inIndex && !inHead:
			snap.add(p, &snapFile{
				path: p, staged: true, stagedKind: diff.Added,
				indexHash: ie.Hash,
			})
			added = append(added, p)
		case !inIndex && inHead:
			snap.add(p, &snapFile{
				path: p, staged: true, stagedKind: diff.Deleted,
				headHash: he.hash,
			})
			deleted = append(deleted, p)
		case inIndex && inHead:
			if ie.Hash != he.hash || ie.Mode != he.mode {
				snap.add(p, &snapFile{
					path: p, staged: true, stagedKind: diff.Modified,
					headHash: he.hash, indexHash: ie.Hash,
					modeChanged: ie.Mode != he.mode,
				})
			}
		}
	}
	snap.detectRenames(added, deleted, headMap)

	// ---- worktree: index vs disk, one stat-first walk.
	rc.walkWorktree(snap, indexMap, idxMod)

	for _, f := range snap.files {
		if f.staged {
			snap.staged = append(snap.staged, FileStatus{
				Path:    f.path,
				OldPath: f.oldPath,
				Letter:  letterFor(f.stagedKind),
				Staged:  true,
			})
		}
	}
	for _, f := range snap.files {
		if f.untracked {
			snap.unstaged = append(snap.unstaged, FileStatus{
				Path: f.path, Letter: "?", Untracked: true,
			})
			continue
		}
		if f.unstagedKind != diff.Modified && f.unstagedKind != diff.Added && f.unstagedKind != diff.Deleted {
			continue
		}
		if f.workHash.IsZero() && f.unstagedKind == diff.Modified {
			continue
		}
		snap.unstaged = append(snap.unstaged, FileStatus{
			Path:   f.path,
			Letter: letterFor(f.unstagedKind),
		})
	}

	sortStatus(snap.staged)
	sortStatus(snap.unstaged)
	return snap, nil
}

// add stores or merges a file entry. A path can be staged and unstaged at the
// same time.
func (s *snapshot) add(path string, f *snapFile) {
	if existing, ok := s.files[path]; ok {
		if f.staged {
			existing.staged = true
			existing.stagedKind = f.stagedKind
			existing.headHash = f.headHash
			existing.indexHash = f.indexHash
			existing.oldPath = f.oldPath
		} else {
			existing.untracked = existing.untracked || f.untracked
			if f.unstagedKind != 0 {
				existing.unstagedKind = f.unstagedKind
			}
			if !f.workHash.IsZero() {
				existing.workHash = f.workHash
			}
		}
		return
	}
	s.files[path] = f
}

// detectRenames turns exact add/delete twins into staged renames, like git
// reports them.
func (s *snapshot) detectRenames(added, deleted []string, headMap map[string]treeEntry) {
	byHash := make(map[plumbing.Hash][]string, len(deleted))
	for _, p := range deleted {
		he := headMap[p]
		byHash[he.hash] = append(byHash[he.hash], p)
	}
	for _, addPath := range added {
		f := s.files[addPath]
		if f == nil || f.indexHash.IsZero() {
			continue
		}
		cands := byHash[f.indexHash]
		if len(cands) == 0 {
			continue
		}
		oldPath := cands[0]
		byHash[f.indexHash] = cands[1:]

		f.stagedKind = diff.Renamed
		f.oldPath = oldPath
		f.headHash = headMap[oldPath].hash
		delete(s.files, oldPath)
	}
}

// walkWorktree compares the index against the filesystem. Ignored directories
// are pruned before descending and unreadable paths are skipped, so a stray
// data directory can neither slow the walk nor abort it.
func (rc *repoContext) walkWorktree(snap *snapshot, indexMap map[string]*index.Entry, idxMod time.Time) {
	root := rc.worktree.Filesystem.Root()

	trackedDirs := make(map[string]bool, len(indexMap))
	for p := range indexMap {
		dir := filepath.ToSlash(filepath.Dir(p))
		for dir != "." && dir != "/" && dir != "" && !trackedDirs[dir] {
			trackedDirs[dir] = true
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
	}

	matcher := rc.ignoreMatcher()

	seen := make(map[string]bool, len(indexMap))

	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entries are skipped, never fatal.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		// A linked worktree or a submodule checkout holds .git as a file, and
		// nested repositories show up the same way: never a tracked path.
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !trackedDirs[rel] && matcher.Match(strings.Split(rel, "/"), true) {
				return filepath.SkipDir
			}
			return nil
		}

		entry, tracked := indexMap[rel]
		if !tracked && matcher.Match(strings.Split(rel, "/"), false) {
			return nil
		}
		if !tracked {
			snap.add(rel, &snapFile{path: rel, untracked: true, unstagedKind: diff.Added})
			return nil
		}

		seen[rel] = true
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if info.IsDir() {
			// Gitlink/submodule: not diffable as text.
			return nil
		}

		if h, changed, modeChanged := rc.worktreeChanged(p, info, entry, idxMod); changed {
			f := &snapFile{
				path: rel, unstagedKind: diff.Modified,
				indexHash: entry.Hash, modeChanged: modeChanged,
			}
			f.workHash = h
			snap.add(rel, f)
		}
		return nil
	})

	// Deleted: tracked paths the walk never saw.
	for p, entry := range indexMap {
		if seen[p] || !entry.Mode.IsFile() {
			continue
		}
		snap.add(p, &snapFile{
			path: p, unstagedKind: diff.Deleted,
			indexHash: entry.Hash, headHash: entry.Hash,
		})
	}
}

// worktreeChanged reports whether a tracked file differs from its index entry,
// hashing only when the metadata cannot prove it clean.
func (rc *repoContext) worktreeChanged(full string, info os.FileInfo, entry *index.Entry, idxMod time.Time) (hash plumbing.Hash, changed, modeChanged bool) {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return plumbing.ZeroHash, false, false
		}
		h := hashString(target)
		return h, h != entry.Hash, false
	}
	if !info.Mode().IsRegular() {
		return plumbing.ZeroHash, false, false
	}

	exec := info.Mode().Perm()&0o100 != 0
	modeChanged = exec != (entry.Mode == filemode.Executable)

	// Racy-git: a file whose mtime is at or after the index write may have been
	// changed within the same timestamp tick, so it is verified by hashing.
	// An unknown index mtime means "assume racy" rather than trust the stat.
	racy := idxMod.IsZero() || !info.ModTime().Before(idxMod)
	metaClean := info.Size() == int64(entry.Size) &&
		info.ModTime().Equal(entry.ModifiedAt) &&
		!modeChanged && !racy
	if metaClean {
		return plumbing.ZeroHash, false, false
	}

	h, err := hashWorktreeFile(full)
	if err != nil {
		return plumbing.ZeroHash, false, false
	}
	if h == entry.Hash && !modeChanged {
		// Content is unchanged; a stat refresh would make the next walk cheap,
		// but we never write the index, so just report clean.
		return plumbing.ZeroHash, false, false
	}
	return h, true, modeChanged
}

// fileFor returns the diff inputs for one path.
func (s *snapshot) fileFor(path string, staged bool) (*snapFile, bool) {
	f, ok := s.files[path]
	if !ok {
		return nil, false
	}
	if staged && !f.staged {
		return nil, false
	}
	if !staged && f.stagedOnly() {
		return nil, false
	}
	return f, true
}

func (f *snapFile) stagedOnly() bool {
	return f.staged && !f.untracked && f.unstagedKind == 0
}

// diffFile builds (or returns cached) the diff of one snapshot entry.
func (rc *repoContext) diffFile(f *snapFile, staged bool, opts Options) (*diff.File, error) {
	kind := f.unstagedKind
	if staged {
		kind = f.stagedKind
	}
	if kind == 0 {
		return nil, fmt.Errorf("no change for %q", f.path)
	}

	oldHash, newHash := f.indexHash, plumbing.ZeroHash
	if staged {
		oldHash, newHash = f.headHash, f.indexHash
	}

	key := fmt.Sprintf("%s|%s|%d|%t|%s|%t|%d", oldHash, newHash, opts.Context,
		opts.IgnoreWhitespace, f.path, staged, kind)
	if cached, ok := rc.diffs.get(key); ok {
		return cached, nil
	}

	var oldText, newText string
	var err error
	if staged {
		if oldText, err = rc.blobContent(f.headHash); err != nil {
			return nil, err
		}
		if newText, err = rc.blobContent(f.indexHash); err != nil {
			return nil, err
		}
	} else {
		if oldText, err = rc.blobContent(f.indexHash); err != nil {
			return nil, err
		}
		if kind == diff.Deleted {
			newText = ""
		} else if newText, err = rc.worktreeContent(f.path); err != nil {
			// An unreadable file is an error, not an empty file: showing the
			// whole content as deleted would be a lie.
			return nil, fmt.Errorf("read %q: %w", f.path, err)
		}
	}

	file := diff.Generate(f.path, kind, oldText, newText, generateOptions(opts))
	file.OldPath = f.path
	file.NewPath = f.path
	if staged && f.oldPath != "" {
		file.OldPath = f.oldPath
	}
	if f.modeChanged && len(file.Hunks) == 0 {
		file.Lines = append(file.Lines, diff.Line{
			Kind: diff.NoNewline,
			Text: "file mode changed",
		})
	}
	rc.diffs.put(key, file)
	return file, nil
}

// statusFiles diffs one column of the snapshot.
func (rc *repoContext) statusFiles(staged bool, opts Options) ([]*diff.File, error) {
	snap, err := rc.snapshotFor(false)
	if err != nil {
		return nil, err
	}

	var files []*diff.File
	for _, f := range snap.files {
		if staged != f.staged {
			continue
		}
		if !staged && (f.untracked || f.unstagedKind == 0) {
			continue
		}
		if staged && !f.staged {
			continue
		}
		file, err := rc.diffFile(f, staged, opts)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	sortFiles(files)
	return files, nil
}

func (rc *repoContext) worktreeFiles(opts Options) ([]*diff.File, error) {
	return rc.statusFiles(false, opts)
}

func (rc *repoContext) stagedFiles(opts Options) ([]*diff.File, error) {
	return rc.statusFiles(true, opts)
}

// indexLocked returns the decoded index, decoding it again whenever the file
// on disk changed. Commits, resets and stash operations run outside twig, so a
// cached decode must never outlive the file it came from. The caller holds
// rc.mu.
func (rc *repoContext) indexLocked() (*index.Index, time.Time, error) {
	// Stat before decoding: if the index is rewritten while we read it, the
	// next refresh sees a different mtime and decodes again.
	mod := rc.indexModTime()
	if rc.index != nil && mod.Equal(rc.indexMod) {
		return rc.index, rc.indexMod, nil
	}

	idx, err := rc.repo.Storer.Index()
	if err != nil {
		return nil, time.Time{}, err
	}
	rc.index = idx
	rc.indexMod = mod
	return idx, rc.indexMod, nil
}

func letterFor(kind diff.ChangeKind) string {
	switch kind {
	case diff.Added:
		return "A"
	case diff.Deleted:
		return "D"
	case diff.Renamed:
		return "R"
	case diff.Copied:
		return "C"
	default:
		return "M"
	}
}

var _ = errors.Is
