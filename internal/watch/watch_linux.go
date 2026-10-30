//go:build linux

package watch

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The mask is deliberately narrow. Reads and attribute changes say nothing
// about a file's contents, and a build rewriting metadata would otherwise keep
// the view refreshing for no reason. IN_EXCL_UNLINK keeps events for files
// already unlinked out of the queue, which is most of the noise in a temporary
// directory.
const watchMask = unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_CREATE |
	unix.IN_DELETE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM |
	unix.IN_DELETE_SELF | unix.IN_MOVE_SELF |
	unix.IN_EXCL_UNLINK | unix.IN_ONLYDIR

// inotify is one watch descriptor set on one inotify instance, with an eventfd
// alongside it so a blocked poll can be woken by Close.
type inotify struct {
	fd     int
	stopfd int
	buf    []byte
}

func newBackend() (backend, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	stopfd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("inotify: %w", err)
	}
	return &inotify{
		fd:     fd,
		stopfd: stopfd,
		// One read must be able to drain a whole burst, or the parser would
		// see a truncated record.
		buf: make([]byte, 4096*(unix.SizeofInotifyEvent+256)),
	}, nil
}

func (b *inotify) name() string { return "inotify" }

// limits returns the inotify watch budget this package is willing to use. Files
// are not needed on Linux: a watch on a directory reports IN_MODIFY for its
// children, so content writes are already covered.
func (b *inotify) limits() (dirs, files int) { return 512, 0 }

func (b *inotify) add(path string, isFile bool) (int32, error) {
	if isFile {
		return 0, errors.New("inotify: file watches are not used")
	}
	wd, err := unix.InotifyAddWatch(b.fd, path, watchMask)
	if err != nil {
		return 0, err
	}
	return int32(wd), nil
}

func (b *inotify) remove(id int32) error {
	_, err := unix.InotifyRmWatch(b.fd, uint32(id))
	return err
}

// stop wakes the blocked read. It is separate from close because the reader
// must return before the descriptors are released.
func (b *inotify) stop() error {
	// Any byte wakes the poll; the value itself is never read.
	_, err := unix.Write(b.stopfd, []byte{1, 0, 0, 0, 0, 0, 0, 0})
	if errors.Is(err, unix.EAGAIN) {
		return nil
	}
	return err
}

// read blocks until at least one event arrives, and reports both a kernel
// queue overflow and the watches the kernel removed by itself.
func (b *inotify) read() (readResult, error) {
	fds := []unix.PollFd{
		{Fd: int32(b.fd), Events: unix.POLLIN},
		{Fd: int32(b.stopfd), Events: unix.POLLIN},
	}
	for {
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return readResult{}, err
		}
		if fds[1].Revents != 0 {
			return readResult{}, errStopped
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}

		n, err := unix.Read(b.fd, b.buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return readResult{}, err
		}
		return scanEvents(b.buf[:n]), nil
	}
}

// scanEvents walks the event records in one read and collects what the caller
// has to act on beyond "something changed": a queue overflow, and watches the
// kernel dropped because their directory went away.
func scanEvents(buf []byte) readResult {
	var res readResult
	for len(buf) >= unix.SizeofInotifyEvent {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[0]))
		if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
			res.overflow = true
		}
		if ev.Mask&unix.IN_IGNORED != 0 {
			res.gone = append(res.gone, ev.Wd)
		}
		size := unix.SizeofInotifyEvent + int(ev.Len)
		if size > len(buf) {
			break
		}
		buf = buf[size:]
	}
	return res
}

// close releases both descriptors. The reader has already returned by the time
// this runs.
func (b *inotify) close() error {
	err := unix.Close(b.fd)
	_ = unix.Close(b.stopfd)
	if errors.Is(err, unix.EBADF) {
		return nil
	}
	return err
}
