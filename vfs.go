package vfs // import "textmodes.com/vfs"

import (
	"io"
	"os"
)

// FileSystem implement a (virtual) file system.
type FileSystem interface {
	Opener

	// Lstat returns the os.FileInfo for the given path, without
	// following symlinks.
	Lstat(path string) (os.FileInfo, error)

	// Stat returns the os.FileInfo for the given path, following
	// symlinks.
	Stat(path string) (os.FileInfo, error)

	// Readdir returns the contents of the directory at path as an slice
	// of os.FileInfo, ordered alphabetically by name. If path is not a
	// directory or the permissions don't allow it, an error will be
	// returned.
	Readdir(path string) ([]os.FileInfo, error)

	// String returns a description of the file system.
	String() string
}

// Opener is a minimal virtual filesystem that can only open regular files.
type Opener interface {
	Open(name string) (ReadSeekCloser, error)
}

// ReadSeekCloser can read, seek and close.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// ReaderAt emulates io.ReaderAt on a ReadSeekCloser by using Seek() for each
// call to ReadAt.
func ReaderAt(rsc ReadSeekCloser) io.ReaderAt {
	return readerAt{rsc}
}

type readerAt struct {
	ReadSeekCloser
}

func (rsc readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	if _, err = rsc.Seek(off, io.SeekStart); err != nil {
		return
	}
	return rsc.Read(p)
}
