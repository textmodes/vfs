package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"textmodes.com/vfs"
	"textmodes.com/vfs/autofs"
)

func main() {
	flag.BoolVar(&vfs.Trace, "trace", false, "enable vfs tracing")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <root>\n", os.Args[0])
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	if vfs.Trace {
		fmt.Fprintf(os.Stderr, "%d open files\n", openFiles())
	}

	fs, err := autofs.New(flag.Arg(0))
	if err != nil {
		panic(err)
	}

	dump(fs, "/", 0)

	if vfs.Trace {
		fmt.Fprintf(os.Stderr, "%d open files\n", openFiles())
	}
}

func dump(fs vfs.FileSystem, base string, depth int) {
	pad := strings.Repeat("  ", depth)

	i, err := fs.Stat(base)
	if err != nil {
		fmt.Printf("%s%s error: %v\n", pad, base, err)
		return
	}

	if !i.IsDir() {
		return
	}

	d, err := fs.Readdir(base)
	if err != nil {
		fmt.Printf("%s%s error: %v\n", pad, base, err)
		return
	}

	//fmt.Printf("%s%s: (%s, %d children, %T: %#+v): %T %#+v\n", base, pad, i.Name(), len(d), fs, fs, i, i)
	//fmt.Printf("%s%s:\n", pad, path.Base(base))
	fmt.Printf("%s %9d %s %s\n", i.Mode(), len(d), i.ModTime().Format("Jan 02 15:04"), base)

	//var dirs []string
	for _, i := range d {
		if i.IsDir() {
			dump(fs, path.Join(base, path.Base(i.Name())), depth+1)
		} else {
			fmt.Printf("%s %9d %s %s\n", i.Mode(), i.Size(), i.ModTime().Format("Jan 02 15:04"), path.Join(base, path.Base(i.Name())))
		}
	}
	if depth == 1 {
		fmt.Println("")
	}
}

func openFiles() int {
	out, err := exec.Command("/bin/sh", "-c", fmt.Sprintf("lsof -p %d", os.Getpid())).Output()
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(out), "\n")
	return len(lines) - 1
}
