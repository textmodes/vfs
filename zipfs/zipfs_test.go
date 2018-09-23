package zipfs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"textmodes.com/vfs"
)

var (

	// files to use to build zip used by zipfs in testing; maps path : contents
	files = map[string]string{"foo": "foo", "bar/baz": "baz", "a/b/c": "c"}

	// expected info for each entry in a file system described by files
	tests = []struct {
		Path      string
		IsDir     bool
		IsRegular bool
		Name      string
		Contents  string
		Files     map[string]bool
	}{
		{"/", true, false, "", "", map[string]bool{"foo": true, "bar": true, "a": true}},
		{"//", true, false, "", "", map[string]bool{"foo": true, "bar": true, "a": true}},
		{"/foo", false, true, "foo", "foo", nil},
		{"/foo/", false, true, "foo", "foo", nil},
		{"/foo//", false, true, "foo", "foo", nil},
		{"/bar", true, false, "bar", "", map[string]bool{"baz": true}},
		{"/bar/", true, false, "bar", "", map[string]bool{"baz": true}},
		{"/bar/baz", false, true, "baz", "baz", nil},
		{"//bar//baz", false, true, "baz", "baz", nil},
		{"/a/b", true, false, "b", "", map[string]bool{"c": true}},
	}

	// to be initialized in setup()
	fs        vfs.FileSystem
	statFuncs []statFunc
)

type statFunc struct {
	Name string
	Func func(string) (os.FileInfo, error)
}

func TestMain(t *testing.M) {
	if err := setup(); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up zipfs testing state: %v.\n", err)
		os.Exit(1)
	}
	os.Exit(t.Run())
}

type testNullCloser struct {
	io.ReadSeeker
	name string
	size int64
}

func (c *testNullCloser) Close() error { return nil }

func (c *testNullCloser) Stat() (os.FileInfo, error) {
	return c, nil
}

func (c testNullCloser) IsDir() bool        { return false }
func (c testNullCloser) ModTime() time.Time { return time.Time{} }
func (c testNullCloser) Mode() os.FileMode  { return 0444 }
func (c testNullCloser) Name() string       { return c.name }
func (c testNullCloser) Size() int64        { return c.size }
func (c testNullCloser) Sys() interface{}   { return nil }

// setups state each of the tests uses
func setup() error {
	// create zipfs
	b := new(bytes.Buffer)
	zw := zip.NewWriter(b)
	for file, contents := range files {
		w, err := zw.Create(file)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, contents)
		if err != nil {
			return err
		}
	}
	zw.Close()
	/*
		zr, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
		if err != nil {
			return err
		}
		fs = New(zr, "foo")
	*/
	opener := func() (fileLike, error) {
		rsc := &testNullCloser{
			ReadSeeker: bytes.NewReader(b.Bytes()),
			name:       "test.zip",
			size:       int64(b.Len()),
		}
		return &emulatedFile{
			ReadSeekCloser: rsc,
			info:           rsc,
			name:           "test.zip",
		}, nil
	}
	fs, _ = open("test.zip", opener)

	// pull out different stat functions
	statFuncs = []statFunc{
		{"Stat", fs.Stat},
		{"Lstat", fs.Lstat},
	}

	return nil
}

func TestReaddir(t *testing.T) {
	for _, test := range tests {
		if test.IsDir {
			infos, err := fs.Readdir(test.Path)
			if err != nil {
				t.Errorf("failed to read directory %s: %v\n", test.Path, err)
				continue
			}
			got := make(map[string]bool)
			for _, info := range infos {
				got[info.Name()] = true
			}
			if want := test.Files; !reflect.DeepEqual(got, want) {
				t.Errorf("expected readdir(%q) to return %v, got %v", test.Path, got, want)
			}
		}
	}
}

func TestStatFuncs(t *testing.T) {
	for _, test := range tests {
		for _, statFunc := range statFuncs {

			// test can stat
			info, err := statFunc.Func(test.Path)
			if err != nil {
				t.Errorf("unexpected error using %v for %v: %v\n", statFunc.Name, test.Path, err)
				continue
			}

			// test info.Name()
			if got, want := info.Name(), test.Name; got != want {
				t.Errorf("Using %v for %v info.Name() got %v wanted %v\n", statFunc.Name, test.Path, got, want)
			}
			// test info.IsDir()
			if got, want := info.IsDir(), test.IsDir; got != want {
				t.Errorf("Using %v for %v info.IsDir() got %v wanted %v\n", statFunc.Name, test.Path, got, want)
			}
			// test info.Mode().IsDir()
			if got, want := info.Mode().IsDir(), test.IsDir; got != want {
				t.Errorf("Using %v for %v info.Mode().IsDir() got %v wanted %v\n", statFunc.Name, test.Path, got, want)
			}
			// test info.Mode().IsRegular()
			if got, want := info.Mode().IsRegular(), test.IsRegular; got != want {
				t.Errorf("Using %v for %v info.Mode().IsRegular() got %v wanted %v\n", statFunc.Name, test.Path, got, want)
			}
			// test info.Size()
			if test.IsRegular {
				if got, want := info.Size(), int64(len(test.Contents)); got != want {
					t.Errorf("Using %v for %v inf.Size() got %v wanted %v", statFunc.Name, test.Path, got, want)
				}
			}
		}
	}
}

func TestNotExist(t *testing.T) {
	_, err := fs.Open("/does-not-exist")
	if err == nil {
		t.Fatalf("Expected an error.\n")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Expected an error satisfying os.IsNotExist: %v\n", err)
	}
}

func TestOpenSeek(t *testing.T) {
	for _, test := range tests {
		if test.IsRegular {
			// test Open()
			f, err := fs.Open(test.Path)
			if err != nil {
				t.Errorf("open(%q) error: %v", test.Path, err)
				return
			}
			defer f.Close()

			// test Seek() multiple times
			for i := 0; i < 3; i++ {
				all, err := ioutil.ReadAll(f)
				if err != nil {
					t.Error(err)
					return
				}
				if got, want := string(all), test.Contents; got != want {
					t.Errorf("File contents for %v got %q wanted %q\n", test.Path, got, want)
				}
				f.Seek(0, 0)
			}
		}
	}
}

type testStat struct {
	name    string
	isDir   bool
	modTime time.Time
	size    int64
}

var testFiles = map[string]map[string]testStat{
	"crc32-not-streamed.zip": map[string]testStat{
		"/bar.txt": testStat{"bar.txt", false, time.Date(2012, 3, 8, 16, 59, 12, 00, time.UTC), 4},
		"/foo.txt": testStat{"foo.txt", false, time.Date(2012, 9, 3, 1, 59, 00, 00, time.UTC), 4},
	},
	"dd.zip": map[string]testStat{
		"/filename": testStat{"filename", false, time.Date(2011, 2, 2, 13, 6, 20, 00, time.UTC), 25},
	},
	"go-no-datadesc-sig.zip": map[string]testStat{
		"/bar.txt": testStat{"bar.txt", false, time.Date(2012, 9, 3, 1, 59, 00, 00, time.UTC), 4},
		"/foo.txt": testStat{"foo.txt", false, time.Date(2012, 9, 3, 1, 59, 00, 00, time.UTC), 4},
	},
}

func TestFile(t *testing.T) {
	names, err := filepath.Glob("../testdata/zip/*.zip")
	if err != nil {
		t.Skip(err)
	}

	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			fs, err := Open(name)
			if err != nil {
				t.Fatalf("error opening %s: %v", name, err)
			}
			testRecursive(t, fs, "/")

			if tests, ok := testFiles[filepath.Base(name)]; ok {
				for name, want := range tests {
					i, err := fs.Stat(name)
					if err != nil {
						t.Fatalf("Stat(%q) error: %v", name, err)
					}
					if v := i.Name(); v != want.name {
						t.Fatalf("expected Stat(%q).Name() to return %q, got %q", name, want.name, v)
					}
					if i.IsDir() != want.isDir {
						t.Fatalf("expected Stat(%q).IsDir() to return %t, got %t", name, want.isDir, i.IsDir())
					}
					if v := i.ModTime(); !want.modTime.Equal(v) {
						t.Fatalf("expected Stat(%q).ModTime() to return %s, got %s", name, want.modTime, v)
					}
					if v := i.Size(); v != want.size {
						t.Fatalf("expected Stat(%q).Size() to return %d, got %d", name, want.size, v)
					}
				}
			} else {
				t.Skip("no testfiles")
			}
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
