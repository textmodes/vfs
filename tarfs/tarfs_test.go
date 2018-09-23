package tarfs_test

import (
	"path"
	"path/filepath"
	"testing"

	"textmodes.com/vfs"
	"textmodes.com/vfs/tarfs"
)

func TestFile(t *testing.T) {
	names, err := filepath.Glob("../testdata/tar/*.tar")
	if err != nil {
		t.Skip(err)
	}

	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			fs, err := tarfs.Open(name)
			if err != nil {
				t.Fatalf("error opening %s: %v", name, err)
			}
			testRecursive(t, fs, "/")
		})
	}
}

func testRecursive(t *testing.T, fs vfs.FileSystem, dir string) {
	t.Helper()

	infos, err := fs.Readdir(dir)
	if err != nil {
		t.Fatalf("Readdir(%q) error: %v", dir, err)
	}

	for _, info := range infos {
		if info.IsDir() {
			testRecursive(t, fs, path.Join(dir, info.Name()))
		}
	}
}
