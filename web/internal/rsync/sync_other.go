//go:build !linux

package rsync

import "os/exec"

// syncFilesystem is a best effort on platforms without syncfs.
func syncFilesystem(path string) error {
	return exec.Command("sync").Run()
}
