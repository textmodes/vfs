package vfs

import (
	"fmt"
	"os"
	"strings"
)

var (
	// Trace enable flag.
	Trace bool
)

// Tracef is a debug helper.
func Tracef(fs FileSystem, format string, v ...interface{}) {
	if Trace {
		format = strings.TrimRight(format, " \t\r\n") + "\n"
		fmt.Fprintf(os.Stderr, "vfs %s: "+format, append([]interface{}{fs}, v...)...)
	}
}
