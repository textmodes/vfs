package vfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

// OS relative to root path.
func OS(root string) FileSystem {
	return osFileSystem{root}
}

type osFileSystem struct {
	root string
}

func (fs osFileSystem) resolve(name string) string {
	// Clean the path so that it cannot possibly begin with ../.
	// If it did, the result of filepath.Join would be outside the
	// tree rooted at root.
	name = path.Clean("/" + name)

	return filepath.Join(fs.root, name)
}

func (fs osFileSystem) Lstat(name string) (os.FileInfo, error) {
	name = fs.resolve(name)
	Tracef(fs, "Lstat(%q)", name)
	return os.Lstat(name)
}

func (fs osFileSystem) Stat(name string) (os.FileInfo, error) {
	name = fs.resolve(name)
	Tracef(fs, "Stat(%q)", name)
	return os.Stat(name)
}

func (fs osFileSystem) Open(name string) (ReadSeekCloser, error) {
	name = fs.resolve(name)
	Tracef(fs, "Open(%q)", name)
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if info.IsDir() {
		f.Close()
		return nil, fmt.Errorf("%s: is a directory", name)
	}

	return f, nil
}

func (fs osFileSystem) Readdir(name string) ([]os.FileInfo, error) {
	Tracef(fs, "Readdir(%q)", fs.resolve(name))
	return ioutil.ReadDir(fs.resolve(name)) // ioutil sorts the output
}

func (fs osFileSystem) String() string {
	return fmt.Sprintf(`osFileSystem(%s)`, fs.root)
}
