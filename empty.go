package vfs

import (
	"errors"
	"os"
	"time"
)

type empty struct{}

// Open implements Opener. Since empty is an empty directory, all attempts to
// open a file will return errors.
func (empty) Open(name string) (ReadSeekCloser, error) {
	if name == "/" {
		return nil, errors.New("open: / is a directory")
	}
	return nil, os.ErrNotExist
}

// Stat returns os.FileInfo for an empty directory if the path is
// is root "/" or error. os.FileInfo is implemented by emptyVFS
func (e empty) Stat(path string) (os.FileInfo, error) {
	if path == "/" {
		return e, nil
	}
	return nil, os.ErrNotExist
}

func (e *empty) Lstat(path string) (os.FileInfo, error) {
	return e.Stat(path)
}

// ReadDir returns an empty os.FileInfo slice for "/", else error.
func (empty) Readdir(path string) ([]os.FileInfo, error) {
	if path == "/" {
		return []os.FileInfo{}, nil
	}
	return nil, os.ErrNotExist
}

func (empty) String() string {
	return "empty(/)"
}

// These functions below implement os.FileInfo for the single
// empty emulated directory.

func (empty) Name() string       { return "/" }
func (empty) Size() int64        { return 0 }
func (empty) Mode() os.FileMode  { return os.ModeDir | os.ModePerm }
func (empty) ModTime() time.Time { return time.Time{} }
func (empty) IsDir() bool        { return true }
func (empty) Sys() interface{}   { return nil }

var _ FileSystem = (*empty)(nil)
