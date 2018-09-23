package tarfs

import (
	"archive/tar"
	"os"
	"time"
)

// fileInfo is the zip-file based implementation of FileInfo
type fileInfo struct {
	name string      // directory-local name
	file *tar.Header // nil for a directory
}

func (fi fileInfo) Name() string {
	return fi.name
}

func (fi fileInfo) Size() int64 {
	if f := fi.file; f != nil {
		return f.Size
	}
	return 0 // directory
}

func (fi fileInfo) ModTime() time.Time {
	if f := fi.file; f != nil {
		return f.ModTime
	}
	return time.Time{} // directory has no modified time entry
}

func (fi fileInfo) Mode() os.FileMode {
	if fi.file == nil {
		// Unix directories typically are executable, hence 555.
		return os.ModeDir | 0555
	}
	// Return original file mode without writable bits, since we're a read
	// only file system.
	return fi.file.FileInfo().Mode() & ^os.FileMode(0222)
}

func (fi fileInfo) IsDir() bool {
	return fi.file == nil || fi.file.FileInfo().IsDir()
}

func (fi fileInfo) Sys() interface{} {
	return fi.file
}
