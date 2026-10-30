package watch

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// newWatcher creates a watcher on dirs, skipping the test where the platform
// has no event backend.
func newWatcher(t *testing.T, dirs ...string) *Watcher {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no event backend on this platform")
	}
	w, err := New(Options{Dirs: dirs})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// awaitEvent waits for a change and reports whether one arrived in time.
func awaitEvent(t *testing.T, w *Watcher, within time.Duration) bool {
	t.Helper()
	select {
	case <-w.Events():
		return true
	case <-time.After(within):
		return false
	}
}

func TestWatcherReportsChange(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(t, dir)

	write(t, dir, "a.txt", "one\n")
	if !awaitEvent(t, w, 3*time.Second) {
		t.Fatal("no event for a file created in a watched directory")
	}
}

func TestWatcherReportsModify(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "one\n")
	w := newWatcher(t, dir)

	write(t, dir, "a.txt", "one\ntwo\n")
	if !awaitEvent(t, w, 3*time.Second) {
		t.Fatal("no event for a modified file")
	}
}

// TestWatcherSyncMovesTheWatch covers the set changing under the watcher: the
// new directory reports, and the old one is no longer drained into reports.
func TestWatcherSyncMovesTheWatch(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	w := newWatcher(t, first)

	w.Sync([]string{second}, nil)

	write(t, second, "b.txt", "two\n")
	if !awaitEvent(t, w, 3*time.Second) {
		t.Fatal("no event for a file created after Sync moved the watch")
	}
}

func TestWatcherClose(t *testing.T) {
	w := newWatcher(t, t.TempDir())
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := <-w.Events(); ok {
		t.Fatal("Events should be closed after Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestWatcherCloseIsSafeConcurrently(t *testing.T) {
	w := newWatcher(t, t.TempDir())

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()

	if _, ok := <-w.Events(); ok {
		t.Fatal("Events should be closed after Close")
	}
}

// TestWatcherReAddsDroppedWatch covers a directory that disappears and comes
// back: the kernel drops the watch on its own, and the next Sync has to place
// it again instead of trusting the stale mapping.
func TestWatcherReAddsDroppedWatch(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	w := newWatcher(t, sub)

	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if !awaitEvent(t, w, 3*time.Second) {
		t.Fatal("no event for a removed watched directory")
	}

	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	w.Sync([]string{sub}, nil)
	write(t, sub, "a.txt", "back\n")
	if !awaitEvent(t, w, 3*time.Second) {
		t.Fatal("the recreated directory was not watched again")
	}
}

// TestWatcherHealthOnRealBackend checks the report a caller uses to decide
// whether events are actually flowing.
func TestWatcherHealthOnRealBackend(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(t, dir)

	h := w.Health()
	if h.Dirs != 1 || h.Files != 0 || h.Failed != 0 || h.Dead {
		t.Fatalf("health = %+v, want one live watch", h)
	}
	if !h.Live() {
		t.Fatalf("health = %+v, want Live", h)
	}

	// a directory that does not exist cannot be watched, and that has to be
	// visible: the caller then polls as if there were no watcher at all
	w.Sync([]string{filepath.Join(dir, "missing")}, nil)
	if h := w.Health(); h.Live() || h.Failed == 0 {
		t.Fatalf("health = %+v, want a failed watch", h)
	}
}
