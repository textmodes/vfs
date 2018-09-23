package zipfs

import (
	"archive/zip"
	"os"
	"time"
)

// fileInfo is the zip-file based implementation of FileInfo
type fileInfo struct {
	name string          // directory-local name
	file *zip.FileHeader // nil for a directory
}

func (fi fileInfo) Name() string {
	return fi.name
}

func (fi fileInfo) Size() int64 {
	if f := fi.file; f != nil {
		return int64(f.UncompressedSize)
	}
	return 0 // directory
}

func (fi fileInfo) ModTime() time.Time {
	if f := fi.file; f != nil {
		return f.ModTime()
	}
	return time.Time{} // directory has no modified time entry
}

func (fi fileInfo) Mode() os.FileMode {
	if fi.file == nil {
		// Unix directories typically are executable, hence 555.
		return os.ModeDir | 0555
	}
	return 0444
}

func (fi fileInfo) IsDir() bool {
	return fi.file == nil
}

func (fi fileInfo) Sys() interface{} {
	return nil
}
