//go:build unix

package rsync

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// removeEmptyDirectories removes empty directories below dir in post-order.
// keepTop keeps the source directory itself (used with "copy contents").
// It never crosses file system boundaries and never follows symlinks.
func removeEmptyDirectories(dir string, keepTop bool) (removed int, left []string) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return 0, nil
	}
	rootDev, _ := deviceOf(info)
	var walk func(path string, isRoot bool) bool
	walk = func(path string, isRoot bool) bool {
		entries, err := os.ReadDir(path)
		if err != nil {
			left = append(left, path)
			return false
		}
		var subdirs []string
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			info, err := os.Lstat(child)
			if err != nil || !info.IsDir() {
				continue
			}
			dev, _ := deviceOf(info)
			if dev != 0 && rootDev != 0 && dev != rootDev {
				left = append(left, child) // a mount point
				continue
			}
			subdirs = append(subdirs, child)
		}
		for _, sub := range subdirs {
			walk(sub, false)
		}
		if isRoot && keepTop {
			return false
		}
		if err := syscall.Rmdir(path); err == nil {
			removed++
			return true
		} else if !os.IsNotExist(err) {
			left = append(left, path)
		}
		return false
	}
	walk(dir, true)
	return removed, outermost(left)
}

func deviceOf(info os.FileInfo) (uint64, bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), true
	}
	return 0, false
}

func outermost(paths []string) []string {
	var out []string
	for _, path := range paths {
		nested := false
		for _, parent := range out {
			if path == parent || strings.HasPrefix(path, parent+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, path)
		}
	}
	return out
}
