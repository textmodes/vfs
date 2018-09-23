package vfs

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

type fileSystem struct {
	root string
	base string
	fs   FileSystem
}

// translate translates path for use in m, replacing old with new.
//
// fileSystem{"/src/pkg", "/src", fs}.translate("/src/pkg/code") == "/src/code".
func (fs fileSystem) translate(name string) string {
	name = path.Clean("/" + name)
	if !hasPathPrefix(name, fs.root) {
		panic("translate " + name + " but old=" + fs.root)
	}
	return path.Join(fs.base, name[len(fs.root):])
}

// Scope is a scoped file system. The file system always has at lease one
// entry; the root at / always exists and is a directory.
type Scope map[string][]fileSystem

// NewScope sets up a scope with / mounted.
func NewScope() Scope {
	scope := make(Scope)
	scope.Bind("/", "/", &empty{}, BindReplace)
	return scope
}

// Chroot alters access to fs by making all file system lookups relative to root.
func Chroot(root string, fs FileSystem) FileSystem {
	scope := NewScope()
	scope.Bind("/", root, fs, BindReplace)
	return scope
}

// BindMode determines where the bound file system is mounted.
type BindMode int

// Bind modes.
const (
	BindReplace BindMode = iota
	BindBefore
	BindAfter
)

// Bind causes references to root to redirect to the path base in fs.
// If mode is BindReplace, root redirections are discarded.
// If mode is BindBefore, this redirection takes priority over existing ones,
// but earlier ones are still consulted for paths that do not exist in fs.
// If mode is BindAfter, this redirection happens only after existing ones
// have been tried and failed.
func (scope Scope) Bind(root, base string, fs FileSystem, mode BindMode) {
	root = scope.clean(root)
	base = scope.clean(base)

	var (
		newFS  = fileSystem{root, base, fs}
		mounts []fileSystem
	)
	switch mode {
	case BindReplace:
		mounts = append(mounts, newFS)
	case BindBefore:
		mounts = append(mounts, newFS)
		mounts = append(mounts, scope.resolve(root)...)
	case BindAfter:
		mounts = append(mounts, scope.resolve(root)...)
		mounts = append(mounts, newFS)
	}

	for _, mount := range mounts {
		if mount.root != root {
			if !hasPathPrefix(root, mount.root) {
				// This should not happen.  If it does, panic so
				// that we can see the call trace that led to it.
				panic(fmt.Sprintf("invalid Bind: root=%q fileSystem{%q, %q, %s}",
					root, mount.root, mount.base, mount.fs.String()))
			}
			suffix := root[len(mount.root):]
			mount.root = path.Join(mount.root, suffix)
			mount.base = path.Join(mount.base, suffix)
		}
	}

	scope[root] = mounts
}

func (scope Scope) resolve(name string) []fileSystem {
	name = scope.clean(name)

	for {
		if m, ok := scope[name]; ok {
			return m
		}
		if name == "/" {
			return nil
		}
		name = path.Dir(name)
	}
}

// Open implements the FileSystem Open method.
func (scope Scope) Open(name string) (ReadSeekCloser, error) {
	var err error
	for _, m := range scope.resolve(name) {
		r, err1 := m.fs.Open(m.translate(name))
		if err1 == nil {
			return r, nil
		}
		// IsNotExist errors in overlay FSes can mask real errors in
		// the underlying FS, so ignore them if there is another error.
		if err == nil || os.IsNotExist(err) {
			err = err1
		}
	}
	if err == nil {
		err = &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	return nil, err
}

// stat implements the FileSystem Stat and Lstat methods.
func (scope Scope) stat(name string, f func(FileSystem, string) (os.FileInfo, error)) (os.FileInfo, error) {
	var err error
	for _, mount := range scope.resolve(name) {
		info, err1 := f(mount.fs, mount.translate(name))
		if err1 == nil {
			return info, nil
		}
		if err == nil {
			err = err1
		}
	}
	if err == nil {
		err = &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	return nil, err
}

// Stat returns a FileInfo describing the named file.
func (scope Scope) Stat(name string) (os.FileInfo, error) {
	Tracef(scope, "Stat(%q)", name)
	return scope.stat(name, FileSystem.Stat)
}

// Lstat returns a FileInfo describing the named file. If the file is a
// symbolic link, the returned FileInfo describes the symbolic link. Lstat
// makes no attempt to follow the link.
func (scope Scope) Lstat(name string) (os.FileInfo, error) {
	Tracef(scope, "Lstat(%q)", name)
	return scope.stat(name, FileSystem.Lstat)
}

// Readdir reads the contents of the directory associated with name.
func (scope Scope) Readdir(name string) ([]os.FileInfo, error) {
	name = scope.clean(name)
	Tracef(scope, "Readdir(%q)", name)

	var (
		haveGo   = false
		haveName = map[string]bool{}
		all      []os.FileInfo
		err      error
		first    []os.FileInfo
	)

	for _, m := range scope.resolve(name) {
		dir, err1 := m.fs.Readdir(m.translate(name))
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}

		if dir == nil {
			dir = []os.FileInfo{}
		}

		if first == nil {
			first = dir
		}

		// If we don't yet have Go files in 'all' and this directory
		// has some, add all the files from this directory.
		// Otherwise, only add subdirectories.
		useFiles := false
		if !haveGo {
			for _, d := range dir {
				if strings.HasSuffix(d.Name(), ".go") {
					useFiles = true
					haveGo = true
					break
				}
			}
		}

		for _, d := range dir {
			name := d.Name()
			if (d.IsDir() || useFiles) && !haveName[name] {
				haveName[name] = true
				all = append(all, d)
			}
		}
	}

	// We didn't find any directories containing Go files.
	// If some directory returned successfully, use that.
	if !haveGo {
		for _, d := range first {
			if !haveName[d.Name()] {
				haveName[d.Name()] = true
				all = append(all, d)
			}
		}
	}

	// Built union.  Add any missing directories needed to reach mount points.
	for old := range scope {
		if hasPathPrefix(old, name) && old != name {
			// Find next element after path in old.
			elem := old[len(name):]
			elem = strings.TrimPrefix(elem, "/")
			if i := strings.Index(elem, "/"); i >= 0 {
				elem = elem[:i]
			}
			if !haveName[elem] {
				haveName[elem] = true
				all = append(all, dirInfo(elem))
			}
		}
	}

	if len(all) == 0 {
		return nil, err
	}

	sort.Sort(byName(all))
	return all, nil
}

// dirInfo is a trivial implementation of os.FileInfo for a directory.
type dirInfo string

func (d dirInfo) Name() string       { return string(d) }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() os.FileMode  { return os.ModeDir | 0555 }
func (d dirInfo) ModTime() time.Time { return time.Time{} }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() interface{}   { return nil }

// byName implements sort.Interface.
type byName []os.FileInfo

func (f byName) Len() int           { return len(f) }
func (f byName) Less(i, j int) bool { return f[i].Name() < f[j].Name() }
func (f byName) Swap(i, j int)      { f[i], f[j] = f[j], f[i] }

func (scope Scope) String() string {
	return "scope"
}

// clean returns a cleaned, rooted path for evaluation
func (scope Scope) clean(name string) string {
	return path.Clean("/" + name)
}

// hasPathPrefix returns true if x == y or x == y + "/" + more
func hasPathPrefix(x, y string) bool {
	return x == y || strings.HasPrefix(x, y) && (strings.HasSuffix(y, "/") || strings.HasPrefix(x[len(y):], "/"))
}
