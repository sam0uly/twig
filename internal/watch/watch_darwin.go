//go:build darwin

package watch

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// userIdent is the EVFILT_USER event used to wake the reader on Close; it
	// costs no extra descriptor.
	userIdent = 1
	// watchEvents is how many vnode events one read returns.
	watchEvents = 64
)

// kqueue watches vnodes, not paths, and charges one descriptor each. A
// directory vnode reports only entry changes, so a save inside a directory is
// invisible unless the file itself is watched: that is why this backend uses
// the file half of the watch set.
type kqueue struct {
	kq   int
	mu   sync.Mutex
	byID map[int32]int // watch id -> descriptor
	byFd map[int]int32 // descriptor -> watch id
	next int32
}

func newBackend() (backend, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("kqueue: %w", err)
	}
	// register the wakeup event before anything can need it
	ev := unix.Kevent_t{
		Ident:  userIdent,
		Filter: unix.EVFILT_USER,
		Flags:  unix.EV_ADD | unix.EV_CLEAR,
	}
	if _, err := unix.Kevent(kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(kq)
		return nil, fmt.Errorf("kqueue: %w", err)
	}
	return &kqueue{
		kq:   kq,
		byID: make(map[int32]int),
		byFd: make(map[int]int32),
	}, nil
}

func (k *kqueue) name() string { return "kqueue" }

// limits keeps the descriptor budget well under the macOS default soft limit
// (often 256), because this backend spends one per watched object.
func (k *kqueue) limits() (dirs, files int) { return 96, 96 }

func (k *kqueue) add(path string, isFile bool) (int32, error) {
	// O_EVTONLY asks for a descriptor that only carries events, which works
	// without read permission and does not block unmounting.
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}

	ev := unix.Kevent_t{
		Ident:  uint64(fd),
		Filter: unix.EVFILT_VNODE,
		Flags:  unix.EV_ADD | unix.EV_CLEAR,
		Fflags: unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE |
			unix.NOTE_RENAME | unix.NOTE_REVOKE,
	}
	if _, err := unix.Kevent(k.kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(fd)
		return 0, err
	}

	k.mu.Lock()
	k.next++
	id := k.next
	k.byID[id] = fd
	k.byFd[fd] = id
	k.mu.Unlock()
	return id, nil
}

func (k *kqueue) remove(id int32) error {
	k.mu.Lock()
	fd, ok := k.byID[id]
	delete(k.byID, id)
	delete(k.byFd, fd)
	k.mu.Unlock()
	if !ok {
		return nil
	}
	// closing releases the watch; an explicit EV_DELETE would race the close
	return unix.Close(fd)
}

// read blocks until a vnode changes. A vnode that went away is reported as
// gone, with its descriptor released, so the caller can place the path again.
func (k *kqueue) read() (readResult, error) {
	events := make([]unix.Kevent_t, watchEvents)
	for {
		n, err := unix.Kevent(k.kq, nil, events, nil)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return readResult{}, err
		}

		var res readResult
		for i := range n {
			ev := events[i]
			if ev.Filter == unix.EVFILT_USER && ev.Ident == userIdent {
				return readResult{}, errStopped
			}
			if ev.Flags&unix.EV_ERROR != 0 {
				continue
			}
			id, ok := k.idFor(int(ev.Ident))
			if !ok {
				continue
			}
			if ev.Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME|unix.NOTE_REVOKE) != 0 {
				// the vnode this descriptor watches is gone: drop it here,
				// because a stale descriptor would never fire again
				_ = k.remove(id)
				res.gone = append(res.gone, id)
				continue
			}
			// NOTE_WRITE covers a write, a truncate and, on a directory, an
			// entry being added, removed or renamed
			return res, nil
		}
		if len(res.gone) > 0 {
			return res, nil
		}
		// only unusable events: wait for the next batch
	}
}

// idFor maps a descriptor back to its watch id.
func (k *kqueue) idFor(fd int) (int32, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	id, ok := k.byFd[fd]
	return id, ok
}

// stop wakes the reader through the user event.
func (k *kqueue) stop() error {
	ev := unix.Kevent_t{
		Ident:  userIdent,
		Filter: unix.EVFILT_USER,
		Fflags: unix.NOTE_TRIGGER,
	}
	_, err := unix.Kevent(k.kq, []unix.Kevent_t{ev}, nil, nil)
	return err
}

// close releases every descriptor. The reader has already returned by the time
// this runs.
func (k *kqueue) close() error {
	k.mu.Lock()
	fds := make([]int, 0, len(k.byID))
	for id, fd := range k.byID {
		fds = append(fds, fd)
		delete(k.byID, id)
		delete(k.byFd, fd)
	}
	k.mu.Unlock()

	for _, fd := range fds {
		_ = unix.Close(fd)
	}
	return unix.Close(k.kq)
}
