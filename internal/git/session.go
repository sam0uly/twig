package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"samouly.fun/twig/internal/diff"
)

// repoContext is the shared repository session: the work tree root, the
// resolved git directory, and the caches every operation reads through.
// Repository state is read by driving the git binary through its
// machine-readable porcelain, so ignore rules, rename detection and the
// index race checks behave exactly like git itself.
type repoContext struct {
	mu sync.Mutex

	cwd    string
	root   string
	gitDir string
	// common is the git directory shared by every worktree, so a linked
	// worktree can still watch the refs and logs the main repository uses.
	common string

	// snapshot is read without taking mu, so a repaint that re-materialises a
	// block never waits for a status run that is already in flight.
	snapshot atomic.Pointer[snapshot]
	// snapStamp records the mtimes of the index and HEAD at the moment the
	// snapshot was taken: a change to either means the repository was used
	// outside twig and the snapshot is stale.
	snapStamp string

	blobs *blobCache
	work  *workCache
	diffs *diffCache
}

// One session per process: resolving the work tree root and git directory is
// a subprocess, so everything shares the same handles and caches.
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

	root, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotARepository, err)
	}
	gitDir, err := runGit(cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		gitDir = ""
	}
	common, err := runGit(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		common = ""
	}

	rc := &repoContext{
		cwd:    cwd,
		root:   strings.TrimSpace(root),
		gitDir: strings.TrimSpace(gitDir),
		common: absDir(cwd, strings.TrimSpace(common)),
		blobs:  newBlobCache(256 << 20),
		work:   newWorkCache(64 << 20),
		diffs:  newDiffCache(diffCacheBytes),
	}

	global = rc
	return rc, nil
}

// invalidate drops the cached snapshot and worktree contents. Called after
// staging, unstaging or committing: the repository changed underneath us.
func invalidate() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if global == nil {
		return
	}
	global.snapshot.Store(nil)
	global.snapStamp = ""
	global.work.reset()
}

// git runs a git subcommand in the session's directory and returns its
// standard output.
func (rc *repoContext) git(args ...string) (string, error) {
	return runGit(rc.cwd, args...)
}

// runGit executes git in dir and returns its standard output. Output comes
// from machine-readable porcelain, so the locale is pinned and git is kept
// from taking optional index locks while twig is only reading.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		os.Environ(),
		"LC_ALL=C",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// zeroOID is the all-zero object id git reports for a side that does not
// exist (an unborn HEAD, an added file, a deleted file).
const zeroOID = "0000000000000000000000000000000000000000"

func isZeroOID(oid string) bool {
	return oid == "" || oid == zeroOID
}

// blob returns a blob's contents by object id, cached. Git errors surface
// raw: callers decide whether one missing blob degrades one band or fails
// the whole view.
func (rc *repoContext) blob(oid string) (string, error) {
	if isZeroOID(oid) {
		return "", nil
	}
	if s, ok := rc.blobs.get(oid); ok {
		return s, nil
	}

	s, err := runGit(rc.cwd, "cat-file", "blob", oid)
	if err != nil {
		return "", err
	}
	rc.blobs.put(oid, s)
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
	full := filepath.Join(rc.root, filepath.FromSlash(path))
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

// stamp returns a fingerprint of the index and HEAD files. A repository used
// outside twig (a commit, a stage, a reset) rewrites one of them.
func (rc *repoContext) stamp() string {
	if rc.gitDir == "" {
		return ""
	}
	var b strings.Builder
	for _, name := range []string{"index", "HEAD"} {
		if info, err := os.Stat(filepath.Join(rc.gitDir, name)); err == nil {
			fmt.Fprintf(&b, "%d:%d;", info.ModTime().UnixNano(), info.Size())
		} else {
			b.WriteString("missing;")
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- caches

// blobCache is a byte-bounded LRU of blob contents, keyed by object id.
type blobCache struct {
	mu    sync.Mutex
	m     map[string]string
	order []string
	bytes int
	limit int
}

func newBlobCache(limit int) *blobCache {
	return &blobCache{m: make(map[string]string), limit: limit}
}

func (c *blobCache) get(oid string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[oid]
	return s, ok
}

func (c *blobCache) put(oid, s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[oid]; ok {
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
	c.m[oid] = s
	c.order = append(c.order, oid)
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

// commonDir is the git directory shared by every worktree of this repository,
// which is where a linked worktree's refs and logs actually live.
func (rc *repoContext) commonDir() string { return rc.common }

// absDir resolves a path git reported relative to cwd into an absolute one,
// leaving an already absolute path alone.
func absDir(cwd, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}
