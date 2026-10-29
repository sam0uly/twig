package git

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"

	"twig/internal/diff"
)

// repoContext is the shared repository session: one open handle, one decoded
// index, one HEAD, and the caches every operation reads through.
type repoContext struct {
	mu sync.Mutex

	cwd      string
	repo     *git.Repository
	worktree *git.Worktree
	head     *object.Commit

	index     *index.Index
	indexMod  time.Time
	indexPath string
	headTreeC *object.Tree
	// snapshot is read without taking mu, so a repaint that re-materialises a
	// block never waits for a status walk that is already in flight.
	snapshot atomic.Pointer[snapshot]

	// matcher caches the repository's ignore rules. ReadPatterns walks the
	// whole tree, so rebuilding it for every status refresh would double the
	// walk for nothing.
	matcher    gitignore.Matcher
	matcherMod time.Time

	blobs *blobCache
	work  *workCache
	diffs *diffCache
}

// One session per process: opening a repository and decoding its index is
// expensive (tens to hundreds of milliseconds on large trees), so everything
// shares the same handles and caches.
var (
	globalMu sync.Mutex
	global   *repoContext
)

// open returns the shared repository session, opening it on first use. The
// session is bound to the working directory it was opened from, so a process
// that moves between repositories (tests do) gets a fresh one.
func open() (*repoContext, error) {
	cwd, _ := os.Getwd()

	globalMu.Lock()
	defer globalMu.Unlock()

	if global != nil && global.cwd == cwd {
		return global, nil
	}

	repo, err := git.PlainOpenWithOptions(".", &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotARepository, err)
	}

	rc := &repoContext{
		cwd:   cwd,
		repo:  repo,
		blobs: newBlobCache(256 << 20),
		work:  newWorkCache(64 << 20),
		diffs: newDiffCache(diffCacheBytes),
	}

	if w, err := repo.Worktree(); err == nil {
		// Never let an unreadable path (a container volume, a root-owned data
		// directory) abort status and diff generation.
		w.Filesystem = lenientFS{w.Filesystem}
		rc.worktree = w
		rc.indexPath = findIndexPath(w.Filesystem.Root())
	}
	rc.head = resolveHead(repo)

	global = rc
	return rc, nil
}

// invalidate drops the cached index, HEAD and status snapshot. Called after
// staging, unstaging or committing: the working tree changed underneath us.
func invalidate() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if global == nil {
		return
	}
	global.mu.Lock()
	defer global.mu.Unlock()
	global.index = nil
	global.indexMod = time.Time{}
	global.headTreeC = nil
	global.snapshot.Store(nil)
	global.matcher = nil
	global.matcherMod = time.Time{}
	global.head = resolveHead(global.repo)
	global.work.reset()
}

func resolveHead(repo *git.Repository) *object.Commit {
	ref, err := repo.Head()
	if err != nil {
		return nil
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil
	}
	return c
}

// findIndexPath locates the index file. Plain repositories keep it in .git;
// linked worktrees and submodule checkouts replace .git with a file that
// points at the real git directory.
func findIndexPath(root string) string {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "index")
	}

	b, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	dir := strings.TrimSpace(rest)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, "index")
}

// indexModTime returns the index file's own mtime, the reference point for the
// racy-git rule. An unknown index means "assume every file is racy", which is
// slower but never misses a change.
func (rc *repoContext) indexModTime() time.Time {
	if rc.indexPath == "" {
		return time.Time{}
	}
	if info, err := os.Stat(rc.indexPath); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// refreshHeadLocked re-resolves HEAD when the repository moved underneath us
// (a commit, checkout or reset run outside twig). The caller holds rc.mu.
func (rc *repoContext) refreshHeadLocked() {
	ref, err := rc.repo.Head()
	if err != nil {
		rc.head = nil
		rc.headTreeC = nil
		return
	}
	if rc.head != nil && rc.head.Hash == ref.Hash() {
		return
	}
	rc.headTreeC = nil
	c, err := rc.repo.CommitObject(ref.Hash())
	if err != nil {
		rc.head = nil
		return
	}
	rc.head = c
}

// refreshHead re-resolves HEAD under the session lock.
func (rc *repoContext) refreshHead() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.refreshHeadLocked()
}

// headTreeLocked returns HEAD's tree; the caller holds rc.mu.
func (rc *repoContext) headTreeLocked() (*object.Tree, error) {
	if rc.headTreeC != nil {
		return rc.headTreeC, nil
	}
	if rc.head == nil {
		return &object.Tree{}, nil
	}
	t, err := rc.head.Tree()
	if err != nil {
		return nil, err
	}
	rc.headTreeC = t
	return t, nil
}

// blobContent returns a blob's contents, cached by object id.
func (rc *repoContext) blobContent(h plumbing.Hash) (string, error) {
	if h.IsZero() {
		return "", nil
	}
	if s, ok := rc.blobs.get(h); ok {
		return s, nil
	}

	blob, err := rc.repo.BlobObject(h)
	if err != nil {
		return "", err
	}
	r, err := blob.Reader()
	if err != nil {
		return "", err
	}
	defer r.Close()

	s, err := readString(r, blob.Size)
	if err != nil {
		return "", err
	}
	rc.blobs.put(h, s)
	return s, nil
}

// readString reads r into a string in one copy. A string cannot be built in
// place from a byte slice, but a builder's buffer becomes the returned string,
// so a large file costs its own size rather than twice that at peak.
func readString(r io.Reader, size int64) (string, error) {
	var b strings.Builder
	if n := int(size); n > 0 && int64(n) == size {
		b.Grow(n)
	}
	if _, err := io.Copy(&b, r); err != nil {
		return "", err
	}
	return b.String(), nil
}

// worktreeContent reads a file from the working tree, caching it by size and
// mtime so repeated selections do not hit the disk. Read errors are reported:
// an unreadable file must not look like an empty one.
func (rc *repoContext) worktreeContent(path string) (string, error) {
	if rc.worktree == nil {
		return "", ErrNotARepository
	}
	full := filepath.Join(rc.worktree.Filesystem.Root(), filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if err != nil {
		return "", err
	}
	if e, ok := rc.work.get(path); ok && e.size == info.Size() && e.mod == info.ModTime().UnixNano() {
		return e.data, nil
	}

	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := readString(f, info.Size())
	if err != nil {
		return "", err
	}
	rc.work.put(path, workEntry{size: info.Size(), mod: info.ModTime().UnixNano(), data: data})
	return data, nil
}

// ignoreMatcher returns the repository's ignore rules, rebuilt only when the
// root .gitignore changed or the session was invalidated. ReadPatterns walks
// the whole tree to collect nested files, so rebuilding it per status refresh
// would double the walk for nothing.
func (rc *repoContext) ignoreMatcher() gitignore.Matcher {
	mod := time.Time{}
	if root := rc.worktree.Filesystem.Root(); root != "" {
		if info, err := os.Stat(filepath.Join(root, ".gitignore")); err == nil {
			mod = info.ModTime()
		}
	}
	if rc.matcher != nil && mod.Equal(rc.matcherMod) {
		return rc.matcher
	}

	patterns, err := gitignore.ReadPatterns(rc.worktree.Filesystem, nil)
	if err != nil {
		// Do not cache a failed read: the next refresh may succeed.
		return gitignore.NewMatcher(nil)
	}
	rc.matcher = gitignore.NewMatcher(patterns)
	rc.matcherMod = mod
	return rc.matcher
}

// hashWorktreeFile returns the git blob hash of a regular file.
func hashWorktreeFile(full string) (plumbing.Hash, error) {
	info, err := os.Stat(full)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	f, err := os.Open(full)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer f.Close()

	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", info.Size())
	if _, err := io.Copy(h, f); err != nil {
		return plumbing.ZeroHash, err
	}
	return plumbing.Hash(h.Sum(nil)), nil
}

// hashString returns the git blob hash of an in-memory string.
func hashString(s string) plumbing.Hash {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(s))
	_, _ = io.WriteString(h, s)
	return plumbing.Hash(h.Sum(nil))
}

// ---------------------------------------------------------------- caches

// blobCache is a byte-bounded LRU of blob contents, keyed by object id.
type blobCache struct {
	mu    sync.Mutex
	m     map[plumbing.Hash]string
	order []plumbing.Hash
	bytes int
	limit int
}

func newBlobCache(limit int) *blobCache {
	return &blobCache{m: make(map[plumbing.Hash]string), limit: limit}
}

func (c *blobCache) get(h plumbing.Hash) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[h]
	return s, ok
}

func (c *blobCache) put(h plumbing.Hash, s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[h]; ok {
		return
	}
	for c.bytes+len(s) > c.limit && len(c.order) > 0 {
		old := c.order[0]
		c.order = c.order[1:]
		if v, ok := c.m[old]; ok {
			c.bytes -= len(v)
			delete(c.m, old)
		}
	}
	if len(s) > c.limit {
		return
	}
	c.m[h] = s
	c.order = append(c.order, h)
	c.bytes += len(s)
}

// workCache caches worktree file contents keyed by their stat signature.
type workEntry struct {
	size int64
	mod  int64
	data string
}

// maxWorkEntries bounds the cache's bookkeeping as well as its bytes: a
// repository of thousands of tiny files costs more in map entries than in
// content.
const maxWorkEntries = 1024

// workCache is a byte-bounded FIFO of worktree contents. The bound is on
// bytes because that is what actually runs out: a single large file would
// otherwise stay resident for the whole session.
type workCache struct {
	mu    sync.Mutex
	m     map[string]workEntry
	order []string
	bytes int
	limit int
}

func newWorkCache(limit int) *workCache {
	return &workCache{m: make(map[string]workEntry), limit: limit}
}

func (c *workCache) get(path string) (workEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[path]
	return e, ok
}

func (c *workCache) put(path string, e workEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.m[path]; ok {
		c.bytes -= len(old.data)
	} else {
		c.order = append(c.order, path)
	}
	c.m[path] = e
	c.bytes += len(e.data)

	for len(c.order) > 0 && (c.bytes > c.limit || len(c.order) > maxWorkEntries) {
		old := c.order[0]
		c.order = c.order[1:]
		if v, ok := c.m[old]; ok {
			c.bytes -= len(v.data)
			delete(c.m, old)
		}
	}
}

func (c *workCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = make(map[string]workEntry)
	c.order = nil
	c.bytes = 0
}

// diffCacheBytes bounds the diffs kept for re-display. A diff pins its rows
// (or, when paged, the sources it rebuilds them from) for as long as it is
// cached, so the bound is on bytes: counting entries alone let a handful of
// huge files hold hundreds of megabytes.
const diffCacheBytes = 64 << 20

// diffCache keys generated diffs by content ids and options, so immutable
// inputs are never diffed twice. limit is in bytes.
type diffCache struct {
	mu    sync.Mutex
	m     map[string]*diff.File
	order []string
	bytes int
	limit int
}

func newDiffCache(limit int) *diffCache {
	return &diffCache{m: make(map[string]*diff.File), limit: limit}
}

func (c *diffCache) get(key string) (*diff.File, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.m[key]
	return f, ok
}

func (c *diffCache) put(key string, f *diff.File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[key]; ok {
		return
	}
	size := f.Bytes()
	if size > c.limit {
		// A diff bigger than the whole budget would evict everything and then
		// itself. The caller holds the file it just asked for anyway.
		return
	}

	c.m[key] = f
	c.order = append(c.order, key)
	c.bytes += size
	for len(c.order) > 0 && c.bytes > c.limit {
		old := c.order[0]
		c.order = c.order[1:]
		if v, ok := c.m[old]; ok {
			c.bytes -= v.Bytes()
			delete(c.m, old)
		}
	}
}

// treeEntry is one flattened tree row.
type treeEntry struct {
	hash plumbing.Hash
	mode filemode.FileMode
}

// treeMap flattens a tree into path -> (oid, mode) for its files. Directory
// entries come back from the walker too, and submodules carry a commit id:
// neither is a blob, so neither may enter a map that later gets read as file
// content (that is what produced phantom staged deletions of directories).
func treeMap(t *object.Tree) (map[string]treeEntry, error) {
	out := make(map[string]treeEntry)
	if t == nil {
		return out, nil
	}
	walker := object.NewTreeWalker(t, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !entry.Mode.IsFile() {
			continue
		}
		out[name] = treeEntry{hash: entry.Hash, mode: entry.Mode}
	}
	return out, nil
}
