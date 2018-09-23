package rarfs_test

import (
	"path"
	"path/filepath"
	"testing"

	"textmodes.com/vfs"
	"textmodes.com/vfs/rarfs"
)

var testPassword = map[string]string{
	"rar3-comment-psw.rar":  "password",
	"rar3-comment-hpsw.rar": "password",
	"rar5-hpsw.rar":         "password",
	"rar5-psw.rar":          "password",
	"rar5-psw-blake.rar":    "password",
}

func TestFile(t *testing.T) {
	names, err := filepath.Glob("../testdata/rar/*.rar")
	if err != nil {
		t.Skip(err)
	}

	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			fs, err := rarfs.Open(name, testPassword[filepath.Base(name)])
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
