package tarfs

import (
	"archive/tar"
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
		vfs.Tracef(nil, "os.Open(%q)", name)
		return os.Open(name)
	})
}

// OpenFile opens a file on a FileSystem as FileSystem.
func OpenFile(fs vfs.FileSystem, name string) (vfs.FileSystem, error) {
	vfs.Tracef(fs, "OpenFile(%q)", name)
	return open(name, func() (fileLike, error) {
		vfs.Tracef(fs, "OpenFile.open(%q)", name)
		i, err := fs.Stat(name)
		if err != nil {
			return nil, err
		}
		f, err := fs.Open(name)
		if err != nil {
			return nil, err
		}
		return emulatedFile{
			ReadSeekCloser: f,
			info:           i,
			name:           name,
		}, nil
	})
}

func open(name string, open func() (fileLike, error)) (vfs.FileSystem, error) {
	z, err := openReadCloser(open)
	if err != nil {
		return nil, err
	}
	defer z.Close()

	var (
		fs = &fileSystem{
			name: name,
			open: open,
		}
		h *tar.Header
	)
reading:
	for {
		if h, err = z.Next(); err != nil {
			if err == io.EOF {
				break reading
			}
			return nil, err
		}
		fs.list = append(fs.list, h)
	}

	sort.SliceStable(fs.list, func(i, j int) bool {
		return clean(fs.list[i].Name) < clean(fs.list[j].Name)
	})

	if err = z.Close(); err != nil {
		return nil, err
	}

	return fs, nil
}

type readCloser struct {
	*tar.Reader
	io.Closer
}

func openReadCloser(open func() (fileLike, error)) (*readCloser, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}

	if _, err = f.Stat(); err != nil {
		f.Close()
		return nil, err
	}

	r, err := maybeDecompress(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	return &readCloser{
		Reader: tar.NewReader(r),
		Closer: f,
	}, nil
}

type fileSystem struct {
	name string
	list []*tar.Header
	open func() (fileLike, error)
}

func isRoot(abspath string) bool {
	return path.Clean(abspath) == "/"
}

func clean(name string) string {
	//name = strings.Replace(name, `\`, `/`, -1)
	return path.Clean("/" + name)[1:]
}

// lookup returns the smallest index of an entry with an exact match
// for name, or an inexact match starting with name/. If there is no
// such entry, the result is -1, false.
func (fs fileSystem) lookup(name string) (index int, exact bool) {
	// look for exact match first (name comes before name/ in z)
	i := sort.Search(len(fs.list), func(i int) bool {
		return name <= clean(fs.list[i].Name)
	})
	log.Printf("lookup(%q): %d", name, i)
	if i >= len(fs.list) {
		return -1, false
	}
	// 0 <= i < len(z)
	if clean(fs.list[i].Name) == name {
		return i, true
	}

	// look for inexact match (must be in z[i:], if present)
	fs.list = fs.list[i:]
	name += "/"
	j := sort.Search(len(fs.list), func(i int) bool {
		return name <= clean(fs.list[i].Name)
	})
	if j >= len(fs.list) {
		return -1, false
	}
	// 0 <= j < len(z)
	if strings.HasPrefix(clean(fs.list[i].Name), name) {
		return i + j, false
	}

	return -1, false
}

func (fs *fileSystem) stat(abspath string) (int, fileInfo, error) {
	if isRoot(abspath) {
		return 0, fileInfo{
			name: "",
			file: nil,
		}, nil
	}
	tarpath := clean(abspath)
	/* , err := tarPath(abspath)
	if err != nil {
		return 0, fileInfo{}, err
	}
	*/
	i, exact := fs.lookup(tarpath)
	if i < 0 {
		// rarpath has leading '/' stripped - print it explicitly
		return -1, fileInfo{}, &os.PathError{Path: "/" + tarpath, Err: os.ErrNotExist}
	}
	_, name := path.Split(tarpath)
	var file *tar.Header
	if exact {
		file = fs.list[i] // exact match found - must be a file
	}
	name = path.Clean("/" + name)
	return i, fileInfo{name, file}, nil
}

func (fs *fileSystem) Lstat(abspath string) (os.FileInfo, error) {
	vfs.Tracef(fs, "Lstat(%q)", abspath)
	_, info, err := fs.stat(abspath)
	return info, err
}

func (fs *fileSystem) Stat(abspath string) (os.FileInfo, error) {
	vfs.Tracef(fs, "Stat(%q) -> %q", abspath, clean(abspath))
	_, info, err := fs.stat(abspath)
	return info, err
}

type emulatedRSC struct {
	io.Closer
	io.Reader
	name string
	open func() (fileLike, error)
}

func (rsc emulatedRSC) Close() error {
	return rsc.Closer.Close()
}

func (rsc emulatedRSC) Read(p []byte) (n int, err error) {
	return rsc.Reader.Read(p)
}

func (rsc emulatedRSC) Seek(offset int64, whence int) (int64, error) {
	if whence == 0 && offset == 0 {
		z, err := rsc.open()
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
	_, info, err := fs.stat(abspath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("tarfs: %s is a directory", abspath)
	}

	z, err := openReadCloser(fs.open)
	if err != nil {
		return nil, err
	}
	defer z.Close()

	var h *tar.Header
	for {
		if h, err = z.Next(); err != nil {
			return nil, err
		}
		if h.Name == info.name {
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
		dirname = "/"
	} else {
		dirname = clean(abspath) + "/"
	}
	prevname := ""
	for _, e := range fs.list[i:] {
		base := path.Clean("/" + e.Name)
		//vfs.Tracef(fs, "readdir(%q): %q in %q?", abspath, e.Name, dirname)
		if !strings.HasPrefix(base, dirname) {
			break // not in the same directory anymore
		}
		name := base[len(dirname):] // local name
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
	return fmt.Sprintf(`tarfs(%q)`, fs.name)
}
