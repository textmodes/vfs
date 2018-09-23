package autofs

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"textmodes.com/vfs"
	"textmodes.com/vfs/rarfs"
	"textmodes.com/vfs/tarfs"
	"textmodes.com/vfs/zipfs"
)

// Common errors.
var (
	ErrNotSupported = errors.New("autofs: not supported")
	ErrDir          = errors.New("autofs: is a directory")

	hasFileSystem = map[string]bool{
		".rar": true,
		".tar": true,
		".zip": true,
	}
)

// New autofs starting at directory root.
func New(root string) (vfs.FileSystem, error) {
	if !filepath.IsAbs(root) {
		var err error
		if root, err = filepath.Abs(root); err != nil {
			return nil, err
		}
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	var (
		fs = &fileSystem{
			Scope:        vfs.NewScope(),
			overlay:      make(map[string]vfs.FileSystem),
			overlayMutex: new(sync.Mutex),
		}
		base vfs.FileSystem
	)
	if info.IsDir() {
		fs.Bind("/", "/", vfs.OS(root), vfs.BindReplace)
	} else {
		fs.Bind("/", "/", vfs.OS(path.Dir(root)), vfs.BindReplace)
		if base, err = fs.openFileSystem(path.Base(root)); err != nil {
			return nil, err
		}
		fs.Bind("/", "/", base, vfs.BindReplace)
	}

	return fs, nil
}

type fileSystem struct {
	vfs.Scope
	overlay      map[string]vfs.FileSystem
	overlayMutex *sync.Mutex
}

func (fs fileSystem) openFileSystem(name string) (vfs.FileSystem, error) {
	info, err := fs.Scope.Stat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrDir
	}

	switch ext := strings.ToLower(filepath.Ext(info.Name())); ext {
	case ".rar":
		return rarfs.OpenFile(fs.Scope, name)
	case ".tar":
		return tarfs.OpenFile(fs.Scope, name)
	case ".zip":
		return zipfs.OpenFile(fs.Scope, name)
	default:
		return nil, ErrNotSupported
	}
}

func (fs fileSystem) Readdir(name string) ([]os.FileInfo, error) {
	name = fs.clean(name)

	dir, base := name, "/"
	for {
		if overlay, ok := fs.overlay[dir]; ok {
			return overlay.Readdir(base)
		}
		if i := strings.LastIndexByte(dir, '/'); i > -1 {
			dir, base = dir[:i], dir[i:]
		} else {
			break
		}
	}

	infos, err := fs.Scope.Readdir(name)
	if err != nil {
		return nil, err
	}

	fs.overlayMutex.Lock()
	defer fs.overlayMutex.Unlock()

probing:
	for i, info := range infos {
		full := path.Join(name, path.Base(info.Name()))
		if _, ok := fs.overlay[full]; ok {
			infos[i] = dirInfo{
				name:    info.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
			}
		} else if hasFileSystem[strings.ToLower(filepath.Ext(full))] {
			vfs.Tracef(fs, "Readdir(%q): mounting %q", name, full)
			if fs.overlay[full], err = fs.openFileSystem(full); err != nil {
				if err == vfs.ErrNotSupported {
					delete(fs.overlay, full)
					continue probing
				}
				return nil, err
			}
			infos[i] = dirInfo{
				name:    info.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
			}
		}
	}

	return infos, nil
}

func (fs fileSystem) clean(name string) string {
	return path.Clean("/" + name)
}

func (fs fileSystem) stat(name string, stat func(string) (os.FileInfo, error)) (os.FileInfo, error) {
	info, err := fs.Scope.Stat(name)
	if err != nil {
		return nil, err
	}

	if hasFileSystem[strings.ToLower(filepath.Ext(info.Name()))] {
		return dirInfo{
			name:    info.Name(),
			size:    info.Size(),
			modTime: info.ModTime(),
		}, nil
	}

	return info, nil
}

func (fs fileSystem) Lstat(name string) (os.FileInfo, error) {
	name = fs.clean(name)
	vfs.Tracef(fs, "Lstat(%q)", name)
	if _, ok := fs.overlay[name]; ok {
		return dirInfo{name: name}, nil
	}
	dir, base := name, "/"
	for {
		if overlay, ok := fs.overlay[dir]; ok {
			return overlay.Lstat(base)
		}
		if i := strings.LastIndexByte(dir, '/'); i > -1 {
			dir, base = dir[:i], dir[i:]
		} else {
			break
		}
	}
	return fs.stat(name, fs.Scope.Lstat)
}

func (fs fileSystem) Stat(name string) (os.FileInfo, error) {
	name = fs.clean(name)
	vfs.Tracef(fs, "Stat(%q)", name)
	if _, ok := fs.overlay[name]; ok {
		return dirInfo{name: name}, nil
	}
	dir, base := name, "/"
	for {
		if overlay, ok := fs.overlay[dir]; ok {
			return overlay.Stat(base)
		}
		if i := strings.LastIndexByte(dir, '/'); i > -1 {
			dir, base = dir[:i], dir[i:]
		} else {
			break
		}
	}
	return fs.stat(name, fs.Scope.Stat)
}

// dirInfo is a trivial implementation of os.FileInfo for a directory.
type dirInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (d dirInfo) Name() string       { return d.name }
func (d dirInfo) Size() int64        { return d.size }
func (d dirInfo) Mode() os.FileMode  { return os.ModeDir | 0555 }
func (d dirInfo) ModTime() time.Time { return d.modTime }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() interface{}   { return nil }
