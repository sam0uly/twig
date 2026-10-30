// Package watch reports coalesced filesystem activity for a watch set.
//
// It is deliberately not recursive. A work tree can hold hundreds of thousands
// of directories and every kernel facility here charges per watched object, so
// watching the tree costs megabytes of kernel memory and seconds of
// registration. The objects whose changes can alter a diff view are a handful:
// the git directory's own state, the directories holding the listed files, and
// on some platforms the files themselves.
package watch

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrUnsupported is returned where the platform has no event backend. Callers
// keep polling; it is not worth surfacing.
var ErrUnsupported = errors.New("watch: unsupported platform")

// errStopped is what a blocked read returns once Close has woken it.
var errStopped = errors.New("watch: closed")

const (
	// defaultQuiet is how long the filesystem must be quiet before a burst is
	// reported as one change. It sits above the ~100ms window editors use to
	// turn a save into a rename, and below the point where an update feels
	// late.
	defaultQuiet = 120 * time.Millisecond
	// maxWait bounds the wait for quiet, so a directory being written
	// continuously still reports progress.
	maxWait = 2 * time.Second
)

// Options configures a Watcher.
type Options struct {
	// Quiet is the quiet period before a burst is reported. Zero means the
	// default.
	Quiet time.Duration
	// Dirs are directories whose entries and direct children matter.
	Dirs []string
	// Files are the files whose contents matter. Only platforms where a
	// directory watch cannot see a content write use these; the rest ignore
	// them, which is why the same set can be passed everywhere.
	Files []string
}

// Health is what the watcher is actually doing. It exists so a caller can tell
// "watching" from "quietly polling", which look identical from the outside and
// differ by an order of magnitude in latency.
type Health struct {
	// Backend names the kernel facility, e.g. "inotify".
	Backend string
	// Dirs and Files are the watches that are in place right now.
	Dirs, Files int
	// Failed is how many targets the last Sync could not place, because the
	// kernel refused them: a watch limit, a permission, a vanished path.
	Failed int
	// Truncated reports that the set was larger than the platform allows, so
	// the tail is not watched. That is not a failure, it is a size the poll
	// covers.
	Truncated bool
	// Dead reports that the backend failed and no event will arrive again.
	Dead bool
}

// Live reports whether kernel events are actually flowing. Anything else means
// the caller is on the polling path and should poll as if no watcher existed.
func (h Health) Live() bool {
	return !h.Dead && h.Failed == 0 && h.Dirs+h.Files > 0
}

// String is for log lines and tests.
func (h Health) String() string {
	switch {
	case h.Dead:
		return h.Backend + " dead"
	case h.Failed > 0:
		return h.Backend + " partial"
	case h.Dirs+h.Files == 0:
		return h.Backend + " empty"
	default:
		return h.Backend + " live"
	}
}

// readResult is one batch the kernel handed back.
type readResult struct {
	// overflow reports that the kernel queue dropped events, so the batch is
	// incomplete and the caller must assume everything changed.
	overflow bool
	// gone lists watches the kernel removed on its own: the object was
	// deleted, renamed, or unmounted.
	gone []int32
}

// backend is the platform event source. read blocks until at least one event
// arrives; stop wakes it without releasing anything; close releases the
// descriptors and must only be called after the reader has returned.
type backend interface {
	// name identifies the facility in a health report.
	name() string
	// limits is how many watches of each kind the platform can hold. A zero
	// for files means the platform does not watch files.
	limits() (dirs, files int)
	// add places one watch and returns its handle.
	add(path string, isFile bool) (int32, error)
	remove(id int32) error
	read() (readResult, error)
	stop() error
	close() error
}

// Watcher reports that one of its targets changed. A burst of writes arrives
// as one signal once the filesystem has been quiet for the quiet period, so a
// formatter touching forty files costs one refresh.
type Watcher struct {
	backend backend
	events  chan struct{}
	raw     chan struct{}
	done    chan struct{}
	wg      sync.WaitGroup
	quiet   time.Duration

	closeOnce sync.Once
	closeErr  error

	// dead records that the backend failed and the watcher will never report
	// anything again. Callers fall back to polling.
	dead atomic.Bool

	mu     sync.Mutex
	dirs   map[string]int32
	files  map[string]int32
	failed int
	// droppedDirs and droppedFiles record a set larger than the platform
	// allows, so the tail of it is not watched.
	droppedDirs, droppedFiles bool
}

// New starts watching opts. A target that cannot be watched is counted and
// skipped rather than failing the whole set: a permission problem on one
// directory must not cost every other watch. It returns ErrUnsupported when the
// platform has no backend.
func New(opts Options) (*Watcher, error) {
	b, err := newBackend()
	if err != nil {
		return nil, err
	}
	return newWithBackend(b, opts), nil
}

// newWithBackend wraps an arbitrary backend, which is how tests drive the paths
// the kernel will not produce on demand.
func newWithBackend(b backend, opts Options) *Watcher {
	w := &Watcher{
		backend: b,
		events:  make(chan struct{}, 1),
		raw:     make(chan struct{}, 1),
		done:    make(chan struct{}),
		quiet:   opts.Quiet,
		dirs:    make(map[string]int32),
		files:   make(map[string]int32),
	}
	if w.quiet <= 0 {
		w.quiet = defaultQuiet
	}
	w.Sync(opts.Dirs, opts.Files)

	w.wg.Add(2)
	go w.readLoop()
	go w.signalLoop()
	return w
}

// Events delivers one value per detected change. Nothing is sent while the
// targets are idle, so a reader parked on it costs nothing. The channel closes
// when the watcher is closed.
func (w *Watcher) Events() <-chan struct{} {
	return w.events
}

// Health reports what is actually in place.
func (w *Watcher) Health() Health {
	w.mu.Lock()
	h := Health{
		Backend:   w.backend.name(),
		Dirs:      len(w.dirs),
		Files:     len(w.files),
		Failed:    w.failed,
		Truncated: w.droppedDirs || w.droppedFiles,
	}
	w.mu.Unlock()
	h.Dead = w.dead.Load()
	return h
}

// Sync makes the watch set exactly dirs+files: new targets are added, targets
// no longer listed are dropped. It never reports an error; failures are counted
// and surface through Health, because a watch that cannot be placed is a target
// the poll backstop still covers.
func (w *Watcher) Sync(dirs, files []string) {
	maxDirs, maxFiles := w.backend.limits()

	wantedDirs, droppedDirs := clampSet(dirs, maxDirs)
	wantedFiles, droppedFiles := clampSet(files, maxFiles)

	w.mu.Lock()
	defer w.mu.Unlock()

	w.failed = 0
	w.droppedDirs, w.droppedFiles = droppedDirs, droppedFiles
	w.syncSet(wantedDirs, w.dirs, false)
	w.syncSet(wantedFiles, w.files, true)
}

// syncSet reconciles one half of the watch set. The caller holds mu.
func (w *Watcher) syncSet(want map[string]struct{}, have map[string]int32, isFile bool) {
	for path, id := range have {
		if _, ok := want[path]; ok {
			continue
		}
		_ = w.backend.remove(id)
		delete(have, path)
	}
	for path := range want {
		if _, ok := have[path]; ok {
			continue
		}
		id, err := w.backend.add(path, isFile)
		if err != nil {
			w.failed++
			continue
		}
		have[path] = id
	}
}

// clampSet turns a wanted set into a map, keeping at most limit entries. It
// reports whether anything was left out.
func clampSet(paths []string, limit int) (map[string]struct{}, bool) {
	if limit <= 0 {
		return nil, len(paths) > 0
	}
	want := make(map[string]struct{}, min(len(paths), limit))
	for _, path := range paths {
		if len(want) >= limit {
			return want, true
		}
		want[path] = struct{}{}
	}
	return want, false
}

// forget drops watches the kernel removed by itself, so the next Sync places
// them again. Without it a directory recreated at the same path would never be
// watched, and a reused handle number could be mistaken for the old one.
func (w *Watcher) forget(ids []int32) {
	if len(ids) == 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for path, id := range w.dirs {
		if containsID(ids, id) {
			delete(w.dirs, path)
		}
	}
	for path, id := range w.files {
		if containsID(ids, id) {
			delete(w.files, path)
		}
	}
}

func containsID(ids []int32, id int32) bool {
	for _, gone := range ids {
		if gone == id {
			return true
		}
	}
	return false
}

// Close stops watching and releases the descriptors. It is safe to call more
// than once and from more than one goroutine.
func (w *Watcher) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		// Wake the parked read and let it return before the descriptors go:
		// closing an fd under a blocked poll can hand the number to another
		// goroutine, after which poll and read address a stranger's file.
		if err := w.backend.stop(); err != nil {
			w.dead.Store(true)
		}
		w.wg.Wait()
		w.closeErr = w.backend.close()
		close(w.events)
	})
	return w.closeErr
}

// readLoop drains the backend. The signal channel has room for one value, so a
// slow reader collapses a burst instead of queueing it.
func (w *Watcher) readLoop() {
	defer w.wg.Done()
	for {
		res, err := w.backend.read()
		if err != nil {
			if !errors.Is(err, errStopped) {
				// The backend is gone: report what we know and let the caller
				// fall back to polling for the rest of the session.
				w.dead.Store(true)
				w.signal()
			}
			return
		}
		if res.overflow {
			// The kernel dropped events, so the partial set cannot be trusted:
			// report now instead of waiting for quiet.
			w.signal()
		}
		if len(res.gone) > 0 {
			w.forget(res.gone)
			w.signal()
		}
		select {
		case w.raw <- struct{}{}:
		default:
		}
	}
}

// signalLoop turns raw activity into one coalesced change per quiet period.
func (w *Watcher) signalLoop() {
	defer w.wg.Done()

	timer := time.NewTimer(w.quiet)
	timer.Stop()
	defer timer.Stop()

	wait := max(maxWait, 4*w.quiet)
	var first time.Time
	armed := false

	for {
		select {
		case <-w.done:
			return
		case _, ok := <-w.raw:
			if !ok {
				return
			}
			if !armed {
				armed = true
				first = time.Now()
			}
			if time.Since(first) >= wait {
				w.signal()
				armed = false
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.quiet)
		case <-timer.C:
			if !armed {
				continue
			}
			armed = false
			w.signal()
		}
	}
}

// signal posts a change without ever blocking the signal loop.
func (w *Watcher) signal() {
	select {
	case w.events <- struct{}{}:
	default:
	}
}
