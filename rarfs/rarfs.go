/*
Package rarfs implements a virtual file system from a RAR compressed archive.
*/
package rarfs

import (
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	rar "github.com/nwaples/rardecode"

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
func Open(name string, password ...string) (vfs.FileSystem, error) {
	var pwd string
	if len(password) > 0 {
		pwd = password[0]
	}
	return open(name, func() (fileLike, string, error) {
		vfs.Tracef(nil, "os.Open(%q)", name)
		f, err := os.Open(name)
		return f, pwd, err
	})
}

// OpenFile opens a file on a FileSystem as FileSystem.
func OpenFile(fs vfs.FileSystem, name string, password ...string) (vfs.FileSystem, error) {
	vfs.Tracef(fs, "OpenFile(%q)", name)
	var pwd string
	if len(password) > 0 {
		pwd = password[0]
	}
	return open(name, func() (fileLike, string, error) {
		vfs.Tracef(fs, "OpenFile.open(%q)", name)
		i, err := fs.Stat(name)
		if err != nil {
			return nil, "", err
		}
		f, err := fs.Open(name)
		if err != nil {
			return nil, "", err
		}
		return emulatedFile{f, i, name}, pwd, nil
	})
}

func open(name string, open func() (fileLike, string, error)) (vfs.FileSystem, error) {
	z, err := openReadCloser(open)
	if err != nil {
		switch err.Error() {
		case "rardecode: bad header crc":
			return nil, vfs.ErrNotSupported
		case "rardecode: unsupported decoder version":
			return nil, vfs.ErrNotSupported
		default:
			return nil, err
		}
	}
	defer z.Close()

	fs := &fileSystem{
		name: name,
		open: open,
	}

	var f *rar.FileHeader
reading:
	for {
		f, err = z.Next()
		//vfs.Tracef(fs, "Open(): %+v %v", f, err)
		if err != nil {
			switch err.Error() {
			case "rardecode: bad header crc":
				return nil, vfs.ErrNotSupported
			case "rardecode: unsupported decoder version":
				return nil, vfs.ErrNotSupported
			case io.EOF.Error():
				break reading
			default:
				return nil, err
			}
		}
		// Ignore special files
		if f.Mode()&(os.ModeDevice|os.ModeSocket|os.ModeNamedPipe|os.ModeSymlink) != 0 {
			vfs.Tracef(fs, "Open(): ignore %q: %v", f.Name, f.Mode())
			continue
		}
		if f.IsDir {
			f.Name += "/"
		}
		fs.list = append(fs.list, f)
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
	*rar.Reader
	io.Closer
}

func openReadCloser(open func() (fileLike, string, error)) (*readCloser, error) {
	f, p, err := open()
	if err != nil {
		return nil, err
	}

	if _, err = f.Stat(); err != nil {
		f.Close()
		return nil, err
	}

	z, err := rar.NewReader(f, p)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &readCloser{z, f}, nil
}

type fileSystem struct {
	name string
	list []*rar.FileHeader
	open func() (fileLike, string, error)
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

func rarPath(name string) (string, error) {
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
	rarpath, err := rarPath(abspath)
	if err != nil {
		return 0, fileInfo{}, err
	}
	i, exact := fs.lookup(rarpath)
	if i < 0 {
		// rarpath has leading '/' stripped - print it explicitly
		return -1, fileInfo{}, &os.PathError{Path: "/" + rarpath, Err: os.ErrNotExist}
	}
	_, name := path.Split(rarpath)
	var file *rar.FileHeader
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
	io.Reader
	name string
	open func() (fileLike, string, error)
}

func (rsc emulatedRSC) Close() error {
	return rsc.Closer.Close()
}

func (rsc emulatedRSC) Read(p []byte) (n int, err error) {
	return rsc.Reader.Read(p)
}

func (rsc emulatedRSC) Seek(offset int64, whence int) (int64, error) {
	if whence == 0 && offset == 0 {
		z, _, err := rsc.open()
		if err != nil {
			return 0, err
		}
		rsc.Closer.Close()
		rsc.Closer = z
		return 0, nil
	}
	return 0, fmt.Errorf("unsupported Seek in %s", rsc.name)
}

func (fs *fileSystem) Open(abspath string) (vfs.ReadSeekCloser, error) {
	vfs.Tracef(fs, "Open(%q)", abspath)
	_, fi, err := fs.stat(abspath)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("rarfs: %s is a directory", abspath)
	}

	z, err := openReadCloser(fs.open)
	if err != nil {
		return nil, err
	}
	defer z.Close()

	var h *rar.FileHeader
	for {
		if h, err = z.Next(); err != nil {
			return nil, err
		}
		if h.Name == fi.name {
			break
		}
	}
	return emulatedRSC{
		Reader: z,
		Closer: z,
		name:   h.Name,
		open:   fs.open,
	}, nil
}

func (fs *fileSystem) Readdir(abspath string) ([]os.FileInfo, error) {
	vfs.Tracef(fs, "Readdir(%q)", abspath)
	i, fi, err := fs.stat(abspath)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("rarfs: %s is not a directory", abspath)
	}

	var list []os.FileInfo

	// make dirname the prefix that file names must start with to be considered
	// in this directory. we must special case the root directory because, per
	// the spec of this package, rar file entries MUST NOT start with /, so we
	// should not append /, as we would in every other case.
	var dirname string
	if isRoot(abspath) {
		dirname = ""
	} else {
		rarpath, err := rarPath(abspath)
		if err != nil {
			return nil, err
		}
		dirname = rarpath + "/"
	}
	prevname := ""
	for _, e := range fs.list[i:] {
		//vfs.Tracef(fs, "readdir(%q): %q in %q?", abspath, e.Name, dirname)
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
	return fmt.Sprintf(`rarfs(%s)`, fs.name)
}
