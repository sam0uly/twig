package git

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-billy/v5"
)

// lenientFS hides unreadable paths from go-git's worktree walk. A repository
// can contain directories owned by another user (container volumes and data
// stores are common offenders); one permission error must not take the whole
// viewer down.
type lenientFS struct{ billy.Filesystem }

// Open returns an empty file when the real one cannot be read.
func (l lenientFS) Open(name string) (billy.File, error) {
	f, err := l.Filesystem.Open(name)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		return emptyFile{name: name}, nil
	}
	return f, err
}

// OpenFile mirrors Open for the generalized variant.
func (l lenientFS) OpenFile(name string, flag int, perm os.FileMode) (billy.File, error) {
	f, err := l.Filesystem.OpenFile(name, flag, perm)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		return emptyFile{name: name}, nil
	}
	return f, err
}

// ReadDir treats an unreadable directory as empty instead of aborting.
func (l lenientFS) ReadDir(path string) ([]os.FileInfo, error) {
	infos, err := l.Filesystem.ReadDir(path)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		return nil, nil
	}
	return infos, err
}

// Lstat reports an unreadable entry as an empty file so the walk continues.
func (l lenientFS) Lstat(name string) (os.FileInfo, error) {
	info, err := l.Filesystem.Lstat(name)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		return emptyInfo{name: filepath.Base(name)}, nil
	}
	return info, err
}

// emptyFile is a readable, empty stand-in for a permission-protected file.
type emptyFile struct{ name string }

func (f emptyFile) Name() string                    { return f.name }
func (emptyFile) Read([]byte) (int, error)          { return 0, io.EOF }
func (emptyFile) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (emptyFile) Write(p []byte) (int, error)       { return len(p), nil }
func (emptyFile) Seek(int64, int) (int64, error)    { return 0, nil }
func (emptyFile) Close() error                      { return nil }
func (emptyFile) Lock() error                       { return nil }
func (emptyFile) Unlock() error                     { return nil }
func (emptyFile) Truncate(int64) error              { return nil }

// emptyInfo describes an unreadable path as an empty regular file.
type emptyInfo struct{ name string }

func (i emptyInfo) Name() string     { return i.name }
func (emptyInfo) Size() int64        { return 0 }
func (emptyInfo) Mode() os.FileMode  { return 0 }
func (emptyInfo) ModTime() time.Time { return time.Time{} }
func (emptyInfo) IsDir() bool        { return false }
func (emptyInfo) Sys() any           { return nil }
