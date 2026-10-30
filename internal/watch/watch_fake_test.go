package watch

import (
	"errors"
	"sync"
)

// fakeBackend is driven by the test instead of the kernel, so the paths that
// are hard to provoke for real, a queue overflow, a watch the kernel drops and
// a backend that dies, are exercised on every platform.
type fakeBackend struct {
	mu       sync.Mutex
	added    map[string]int32
	isFile   map[int32]bool
	fail     map[string]error
	next     int32
	addCount int
	dirs     int
	files    int

	events  chan readResult
	stopCh  chan struct{}
	stopOne sync.Once

	readErr error
	closed  bool
}

func newFakeBackend(dirs int) *fakeBackend {
	return &fakeBackend{
		added:  make(map[string]int32),
		isFile: make(map[int32]bool),
		fail:   make(map[string]error),
		dirs:   dirs,
		events: make(chan readResult, 16),
		stopCh: make(chan struct{}),
	}
}

// name identifies the backend in a health report.
func (f *fakeBackend) name() string { return "fake" }

func (f *fakeBackend) limits() (int, int) { return f.dirs, f.files }
func (f *fakeBackend) add(path string, isFile bool) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.fail[path]; ok {
		return 0, err
	}
	f.next++
	f.addCount++
	f.added[path] = f.next
	f.isFile[f.next] = isFile
	return f.next, nil
}

func (f *fakeBackend) remove(id int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for path, have := range f.added {
		if have == id {
			delete(f.added, path)
			delete(f.isFile, id)
			return nil
		}
	}
	return nil
}

func (f *fakeBackend) read() (readResult, error) {
	select {
	case <-f.stopCh:
		return readResult{}, errStopped
	case res := <-f.events:
		if f.readErr != nil {
			err := f.readErr
			f.readErr = nil
			return readResult{}, err
		}
		return res, nil
	}
}

func (f *fakeBackend) stop() error {
	f.stopOne.Do(func() { close(f.stopCh) })
	return nil
}

func (f *fakeBackend) close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// push hands one batch to the watcher as if the kernel had returned it.
func (f *fakeBackend) push(res readResult) { f.events <- res }

// failRead makes the next read fail, which is how a dead backend looks.
func (f *fakeBackend) failRead(err error) { f.readErr = err }

// idOf returns the handle a path is watched under.
func (f *fakeBackend) idOf(path string) (int32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.added[path]
	return id, ok
}

var errFakeRead = errors.New("fake: read failed")

// adds reports how many add calls the backend has seen, which is how a test
// proves a watch was placed again rather than assumed.
func (f *fakeBackend) adds() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addCount
}

// failWith makes add refuse a path, the way a watch limit does.
func (f *fakeBackend) failWith(path string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[path] = err
}
