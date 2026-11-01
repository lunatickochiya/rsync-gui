//go:build linux

package rsync

import (
	"os"
	"path/filepath"
	"syscall"
)

const msdosSuperMagic = 0x4d44

func isFATFileSystem(path string, dryRun bool) (bool, error) {
	dir := path
	for {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				dir = filepath.Dir(dir)
				continue
			}
			var stat syscall.Statfs_t
			if err := syscall.Statfs(dir, &stat); err != nil {
				return false, err
			}
			return uint32(stat.Type) == msdosSuperMagic, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, err
		}
		dir = parent
	}
}
