/*
Package zipfs implements a virtual file system from a PKZip compressed archive.
*/
package zipfs

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"strings"

	"textmodes.com/vfs"
)

type fileLike interface {
	io.Reader
	io.ReaderAt
	io.Closer

	Name() string
	Stat() (os.FileInfo, error)
}

type emulatedFile struct {
	vfs.ReadSeekCloser
	info os.FileInfo
	name string
}

func (rsc emulatedFile) Stat() (os.FileInfo, error) {
	return rsc.info, nil
}

func (rsc emulatedFile) Name() string {
	return rsc.name
}

func (rsc emulatedFile) ReadAt(p []byte, off int64) (n int, err error) {
	if _, err = rsc.Seek(off, io.SeekStart); err != nil {
		return
	}
	return rsc.Read(p)
}

// Open a name file on disk as FileSystem.
func Open(name string) (vfs.FileSystem, error) {
	return open(name, func() (fileLike, error) {
		return os.Open(name)
	})
}

// OpenFile opens a file on a FileSystem as FileSystem.
func OpenFile(fs vfs.FileSystem, name string) (vfs.FileSystem, error) {
	vfs.Tracef(fs, "OpenFile(%q)", name)
	return open(name, func() (fileLike, error) {
		i, err := fs.Stat(name)
		if err != nil {
			return nil, err
		}
		f, err := fs.Open(name)
		if err != nil {
			return nil, err
		}
		return emulatedFile{f, i, name}, nil
	})
}

func open(name string, open func() (fileLike, error)) (vfs.FileSystem, error) {
	z, err := openReadCloser(open)
	if err != nil {
		return nil, err
	}
	defer z.Close()

	fs := &fileSystem{
		name: name,
		list: make([]*zip.FileHeader, len(z.File)),
		open: open,
	}
	for i, file := range z.File {
		fs.list[i] = &file.FileHeader
	}

	sort.SliceStable(fs.list, func(i, j int) bool {
		return fs.list[i].Name < fs.list[j].Name
	})

	if err = z.Close(); err != nil {
		return nil, err
	}

	return fs, nil
}

type readCloser struct {
	*zip.Reader
	io.Closer
}

func openReadCloser(open func() (fileLike, error)) (*readCloser, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}

	i, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	log.Printf("info: %#+v (%T)", i, i)

	z, err := zip.NewReader(f, i.Size())
	if err != nil {
		f.Close()
		switch err {
		case zip.ErrAlgorithm:
			return nil, vfs.ErrNotSupported
		case zip.ErrFormat:
			return nil, vfs.ErrNotSupported
		default:
			return nil, err
		}
	}

	return &readCloser{z, f}, nil
}

type fileSystem struct {
	name string
	list []*zip.FileHeader
	open func() (fileLike, error)
}

// lookup returns the smallest index of an entry with an exact match
// for name, or an inexact match starting with name/. If there is no
// such entry, the result is -1, false.
func (fs fileSystem) lookup(name string) (index int, exact bool) {
	// look for exact match first (name comes before name/ in z)
	i := sort.Search(len(fs.list), func(i int) bool {
		return name <= fs.list[i].Name
	})
	if i >= len(fs.list) {
		return -1, false
	}
	// 0 <= i < len(z)
	if fs.list[i].Name == name {
		return i, true
	}

	// look for inexact match (must be in z[i:], if present)
	fs.list = fs.list[i:]
	name += "/"
	j := sort.Search(len(fs.list), func(i int) bool {
		return name <= fs.list[i].Name
	})
	if j >= len(fs.list) {
		return -1, false
	}
	// 0 <= j < len(z)
	if strings.HasPrefix(fs.list[j].Name, name) {
		return i + j, false
	}

	return -1, false
}

func zipPath(name string) (string, error) {
	name = path.Clean(name)
	if !path.IsAbs(name) {
		return "", fmt.Errorf("stat: not an absolute path: %s", name)
	}
	return name[1:], nil // strip leading '/'
}

func isRoot(abspath string) bool {
	return path.Clean(abspath) == "/"
}

func (fs *fileSystem) stat(abspath string) (int, fileInfo, error) {
	vfs.Tracef(fs, "stat(%q)", abspath)
	if isRoot(abspath) {
		return 0, fileInfo{
			name: "",
			file: nil,
		}, nil
	}
	zippath, err := zipPath(abspath)
	if err != nil {
		return 0, fileInfo{}, err
	}
	i, exact := fs.lookup(zippath)
	if i < 0 {
		// zippath has leading '/' stripped - print it explicitly
		return -1, fileInfo{}, &os.PathError{Path: "/" + zippath, Err: os.ErrNotExist}
	}
	_, name := path.Split(zippath)
	var file *zip.FileHeader
	if exact {
		file = fs.list[i] // exact match found - must be a file
	}
	return i, fileInfo{name, file}, nil
}

func (fs *fileSystem) Lstat(abspath string) (os.FileInfo, error) {
	vfs.Tracef(fs, "Lstat(%q)", abspath)
	_, fi, err := fs.stat(abspath)
	return fi, err
}

func (fs *fileSystem) Stat(abspath string) (os.FileInfo, error) {
	vfs.Tracef(fs, "Stat(%q)", abspath)
	_, fi, err := fs.stat(abspath)
	return fi, err
}

type emulatedRSC struct {
	io.Closer
	io.ReadCloser
	name string
	open func() (fileLike, error)
}

func (rsc emulatedRSC) Close() error {
	err1 := rsc.ReadCloser.Close()
	err2 := rsc.Closer.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (rsc emulatedRSC) Seek(offset int64, whence int) (int64, error) {
	if whence == 0 && offset == 0 {
		r, err := rsc.open()
		if err != nil {
			return 0, err
		}
		rsc.ReadCloser.Close()
		rsc.ReadCloser = r
		return 0, nil
	}
	return 0, fmt.Errorf("unsupported Seek in %s", rsc.name)
}

func (fs *fileSystem) Open(abspath string) (vfs.ReadSeekCloser, error) {
	_, fi, err := fs.stat(abspath)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("zipfs: %s is a directory", abspath)
	}

	z, err := openReadCloser(fs.open)
	if err != nil {
		return nil, err
	}
	defer z.Close()

	for _, file := range z.File {
		if file.Name == fi.file.Name {
			f, err := file.Open()
			if err != nil {
				return nil, err
			}
			return emulatedRSC{
				ReadCloser: f,
				Closer:     z,
				name:       file.Name,
				open:       fs.open,
			}, nil
		}
	}

	panic("unreachable")
}

func (fs *fileSystem) Readdir(abspath string) ([]os.FileInfo, error) {
	i, fi, err := fs.stat(abspath)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("zipfs: %s is not a directory", abspath)
	}

	var list []os.FileInfo

	// make dirname the prefix that file names must start with to be considered
	// in this directory. we must special case the root directory because, per
	// the spec of this package, zip file entries MUST NOT start with /, so we
	// should not append /, as we would in every other case.
	var dirname string
	if isRoot(abspath) {
		dirname = ""
	} else {
		zippath, err := zipPath(abspath)
		if err != nil {
			return nil, err
		}
		dirname = zippath + "/"
	}
	prevname := ""
	for _, e := range fs.list[i:] {
		if !strings.HasPrefix(e.Name, dirname) {
			break // not in the same directory anymore
		}
		name := e.Name[len(dirname):] // local name
		file := e
		if i := strings.IndexRune(name, '/'); i >= 0 {
			// We infer directories from files in subdirectories.
			// If we have x/y, return a directory entry for x.
			name = name[0:i] // keep local directory name only
			file = nil
		}
		// If we have x/y and x/z, don't return two directory entries for x.
		// TODO(gri): It should be possible to do this more efficiently
		// by determining the (fs.list) range of local directory entries
		// (via two binary searches).
		if name != prevname {
			list = append(list, fileInfo{name, file})
			prevname = name
		}
	}

	return list, nil
}

func (fs *fileSystem) String() string {
	return fmt.Sprintf(`zipfs(%s)`, fs.name)
}
