//go:build linux

package rsync

import (
	"os"

	"golang.org/x/sys/unix"
)

// syncFilesystem flushes the destination file system before verification, so
// that the comparison reads the disk instead of the page cache.
func syncFilesystem(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Syncfs(int(f.Fd()))
}
