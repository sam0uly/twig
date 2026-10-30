//go:build windows

package watch

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

const (
	// notifyFilter is what a save, a create, a delete and a rename all look
	// like. FILE_NOTIFY_CHANGE_LAST_WRITE is the one that sees a content
	// write; SIZE alone misses an edit that keeps the length.
	notifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
		windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE |
		windows.FILE_NOTIFY_CHANGE_SIZE
	// readBuffer is per watched directory and lives in kernel non-paged pool,
	// so it is sized for interesting traffic, not for a build tree.
	readBuffer = 32 << 10
	// stopKey marks the completion packet that wakes the reader on Close.
	stopKey = ^uintptr(0)
)

// winWatch is one directory handle with a read pending against it.
type winWatch struct {
	handle windows.Handle
	buf    []byte
	ov     windows.Overlapped
	n      uint32
}

// win is one completion port with a read pending on every watched directory.
// A single goroutine drains it, so the number of directories costs buffers and
// handles but never goroutines.
type win struct {
	port windows.Handle

	mu      sync.Mutex
	watches map[int32]*winWatch
	handles map[windows.Handle]int32
	next    int32

	closing atomic.Bool
}

func newBackend() (backend, error) {
	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("windows: completion port: %w", err)
	}
	return &win{
		port:    port,
		watches: make(map[int32]*winWatch),
		handles: make(map[windows.Handle]int32),
	}, nil
}

func (w *win) name() string { return "readdirectorychangesw" }

// limits returns the watch budget. A directory handle covers its direct
// children, content writes included, so files are not watched separately.
func (w *win) limits() (dirs, files int) { return 512, 0 }

func (w *win) add(path string, isFile bool) (int32, error) {
	if isFile {
		return 0, errors.New("windows: file watches are not used")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return 0, err
	}

	w.mu.Lock()
	w.next++
	id := w.next
	st := &winWatch{handle: handle, buf: make([]byte, readBuffer)}
	w.watches[id] = st
	w.handles[handle] = id
	w.mu.Unlock()

	if _, err := windows.CreateIoCompletionPort(handle, w.port, uintptr(id), 0); err != nil {
		w.remove(id)
		return 0, err
	}
	if err := w.arm(st); err != nil {
		w.remove(id)
		return 0, err
	}
	return id, nil
}

// arm posts the next read. It must always have one pending, or the window
// between two reads is a window where events are lost.
func (w *win) arm(st *winWatch) error {
	err := windows.ReadDirectoryChanges(
		st.handle,
		&st.buf[0],
		uint32(len(st.buf)),
		false, // direct children only; the set is already explicit
		notifyFilter,
		&st.n,
		&st.ov,
		0,
	)
	if err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
		return err
	}
	return nil
}

func (w *win) remove(id int32) error {
	w.mu.Lock()
	st := w.watches[id]
	delete(w.watches, id)
	if st != nil {
		delete(w.handles, st.handle)
	}
	w.mu.Unlock()
	if st == nil {
		return nil
	}
	// cancel first: a pending read holds the handle open and the buffer alive
	_ = windows.CancelIoEx(st.handle, nil)
	return windows.CloseHandle(st.handle)
}

// read blocks on the completion port and reports one batch. Stale completions,
// from a handle that was removed while its read was in flight, are skipped
// rather than reported as activity.
func (w *win) read() (readResult, error) {
	for {
		var n uint32
		var key uintptr
		var ov *windows.Overlapped
		err := windows.GetQueuedCompletionStatus(w.port, &n, &key, &ov, windows.INFINITE)

		if key == stopKey {
			return readResult{}, errStopped
		}
		if w.closing.Load() {
			return readResult{}, errStopped
		}

		w.mu.Lock()
		id := int32(key)
		st := w.watches[id]
		w.mu.Unlock()

		if err != nil {
			// The handle is gone (directory deleted, unmounted): forget it so
			// the next Sync can place it again.
			if st != nil {
				_ = w.remove(id)
				return readResult{gone: []int32{id}}, nil
			}
			continue
		}
		if st == nil {
			// a completion for a watch removed while the read was pending
			continue
		}

		// re-arm before anything else, so the next event is not missed while
		// the caller decides what to do with this one
		armErr := w.arm(st)
		if armErr != nil {
			_ = w.remove(id)
			return readResult{gone: []int32{id}}, nil
		}
		if n == 0 {
			// documented: a zero-length completion means the buffer
			// overflowed and the record of what changed is gone
			return readResult{overflow: true}, nil
		}
		return readResult{}, nil
	}
}

// stop wakes the reader without releasing anything.
func (w *win) stop() error {
	return windows.PostQueuedCompletionStatus(w.port, 0, stopKey, nil)
}

// close releases every handle. The reader has already returned by the time
// this runs.
func (w *win) close() error {
	w.closing.Store(true)

	w.mu.Lock()
	stale := make([]*winWatch, 0, len(w.watches))
	for _, st := range w.watches {
		stale = append(stale, st)
	}
	w.watches = make(map[int32]*winWatch)
	w.handles = make(map[windows.Handle]int32)
	w.mu.Unlock()

	for _, st := range stale {
		_ = windows.CancelIoEx(st.handle, nil)
		_ = windows.CloseHandle(st.handle)
	}
	return windows.CloseHandle(w.port)
}
