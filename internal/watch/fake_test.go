package watch

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// newFake builds a Watcher around a fake backend, so the paths the kernel will
// not produce on demand are covered on every platform.
func newFake(t *testing.T, b *fakeBackend, opts Options) *Watcher {
	t.Helper()
	w := newWithBackend(b, opts)
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// TestOverflowReportsWithoutWaiting covers the kernel telling us it dropped
// events: that has to surface now, not after the quiet period, because the
// partial set cannot be trusted.
func TestOverflowReportsWithoutWaiting(t *testing.T) {
	b := newFakeBackend(8)
	w := newFake(t, b, Options{Dirs: []string{"/a"}, Quiet: time.Second})

	b.push(readResult{overflow: true})

	select {
	case <-w.Events():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("an overflow waited for the quiet period")
	}
}

// TestQuietPeriodIsConfigurable covers the knob: a short quiet period has to
// make a burst land sooner, which is the whole point of exposing it.
func TestQuietPeriodIsConfigurable(t *testing.T) {
	b := newFakeBackend(8)
	w := newFake(t, b, Options{Dirs: []string{"/a"}, Quiet: 20 * time.Millisecond})

	start := time.Now()
	b.push(readResult{})
	select {
	case <-w.Events():
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	if waited := time.Since(start); waited > 500*time.Millisecond {
		t.Fatalf("waited %v with a 20ms quiet period", waited)
	}
}

// TestDroppedWatchIsPlacedAgain covers the kernel retiring a watch on its own:
// the path must leave the set, so the next Sync adds it instead of trusting a
// handle that no longer exists.
func TestDroppedWatchIsPlacedAgain(t *testing.T) {
	b := newFakeBackend(8)
	w := newFake(t, b, Options{Dirs: []string{"/a"}, Quiet: 10 * time.Millisecond})

	id, ok := b.idOf("/a")
	if !ok {
		t.Fatal("the fake did not record the watch")
	}
	if got := b.adds(); got != 1 {
		t.Fatalf("adds = %d, want 1", got)
	}

	b.push(readResult{gone: []int32{id}})
	if !awaitEvent(t, w, time.Second) {
		t.Fatal("a dropped watch was not reported as a change")
	}

	w.Sync([]string{"/a"}, nil)
	if got := b.adds(); got != 2 {
		t.Fatalf("adds = %d, want the watch placed again", got)
	}
	if h := w.Health(); h.Dirs != 1 {
		t.Fatalf("health = %+v, want one dir", h)
	}
}

// TestSetLargerThanThePlatformAllows covers the cap: the tail is not watched,
// which is reported as truncation rather than as a failure, because polling
// covers the rest and the watches that are placed still work.
func TestSetLargerThanThePlatformAllows(t *testing.T) {
	b := newFakeBackend(2)
	w := newFake(t, b, Options{Dirs: []string{"/a", "/b", "/c"}})

	h := w.Health()
	if h.Dirs != 2 {
		t.Fatalf("health = %+v, want the cap applied", h)
	}
	if !h.Truncated {
		t.Fatalf("health = %+v, want Truncated", h)
	}
	if !h.Live() {
		t.Fatalf("health = %+v, a truncated set is still live", h)
	}
}

// TestUnplaceableWatchIsVisible covers the watch limit: an add the kernel
// refuses must not look like a healthy watch.
func TestUnplaceableWatchIsVisible(t *testing.T) {
	b := newFakeBackend(8)
	b.failWith("/b", errors.New("no space left on device"))
	w := newFake(t, b, Options{Dirs: []string{"/a", "/b"}})

	h := w.Health()
	if h.Dirs != 1 || h.Failed != 1 {
		t.Fatalf("health = %+v, want one watch and one failure", h)
	}
	if h.Live() {
		t.Fatal("a refused watch reported itself as live")
	}
}

// TestDeadBackendIsVisible covers the reader failing: the caller has to learn
// that no event will arrive again, and still get one last refresh.
func TestDeadBackendIsVisible(t *testing.T) {
	b := newFakeBackend(8)
	w := newFake(t, b, Options{Dirs: []string{"/a"}, Quiet: 10 * time.Millisecond})

	b.failRead(errFakeRead)
	b.push(readResult{})

	if !awaitEvent(t, w, time.Second) {
		t.Fatal("a dead backend did not report a change")
	}
	deadline := time.Now().Add(time.Second)
	for !w.Health().Dead {
		if time.Now().After(deadline) {
			t.Fatalf("health = %+v, want Dead", w.Health())
		}
		time.Sleep(time.Millisecond)
	}
	if h := w.Health(); h.Live() {
		t.Fatalf("health = %+v, want not Live", h)
	}
}

// TestSyncWhileClosing covers the teardown racing the model: Sync comes from
// the UI goroutine, Close from the same one, but a reload can be in flight.
func TestSyncWhileClosing(t *testing.T) {
	b := newFakeBackend(8)
	w := newFake(t, b, Options{Dirs: []string{"/a"}})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 50 {
			w.Sync([]string{"/a", "/b", string(rune('c' + i%2))}, nil)
		}
	}()
	go func() {
		defer wg.Done()
		_ = w.Close()
	}()
	wg.Wait()

	// a Sync after Close is harmless: the watches simply cannot be placed
	w.Sync([]string{"/z"}, nil)
}
