package tarfs

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"io"

	"github.com/ulikunitz/xz"
)

type decompressor func(r io.Reader) (io.Reader, error)

func bzip2Decompressor(r io.Reader) (io.Reader, error) {
	return bzip2.NewReader(r), nil
}

func lzmaDecompressor(r io.Reader) (io.Reader, error) {
	return xz.NewReader(r)
}

func gzipDecompressor(r io.Reader) (io.Reader, error) {
	return gzip.NewReader(r)
}

// Match reports whether magic matches b. Magic may contain "?" wildcards.
func match(magic string, b []byte) bool {
	if len(magic) != len(b) {
		return false
	}
	for i, c := range b {
		if magic[i] != c && magic[i] != '?' {
			return false
		}
	}
	return true
}

var decompressors = map[string]decompressor{
	"BZh??":        bzip2Decompressor,
	"\x1f\x8b":     gzipDecompressor,
	"\xfd7zXZ\x00": lzmaDecompressor,
}

// maybeDecompress returns a transparent decompressed Reader based on rd's magic
func maybeDecompress(rd io.Reader) (io.Reader, error) {
	r := bufio.NewReader(rd)
	for magic, decomp := range decompressors {
		b, err := r.Peek(len(magic))
		if err != nil {
			return nil, err
		}
		if match(magic, b) {
			return decomp(r)
		}
	}
	return r, nil
}
