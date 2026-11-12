//go:build !windows

package settings

import "os"

// replaceFile renames tmp over path; POSIX rename replaces atomically even
// while other processes hold path open.
func replaceFile(tmp, path string) error { return os.Rename(tmp, path) }
