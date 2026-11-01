//go:build !unix

package rsync

// removeEmptyDirectories is only implemented on Unix. Windows builds keep the
// moved source directories in place instead of risking a wrong cleanup.
func removeEmptyDirectories(dir string, keepTop bool) (int, []string) {
	return 0, []string{dir}
}
