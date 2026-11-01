//go:build !linux

package rsync

// isFATFileSystem is only implemented on Linux (WSL included). Other platforms
// never get an automatic --modify-window.
func isFATFileSystem(path string, dryRun bool) (bool, error) { return false, nil }
