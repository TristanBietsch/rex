//go:build !windows

package settings

import "os"

// replaceFile renames tmp over path; POSIX rename replaces atomically even
// while other processes hold path open.
func replaceFile(tmp, path string) error { return os.Rename(tmp, path) }

// readConfig reads path; a POSIX rename never blocks a reader.
func readConfig(path string) ([]byte, error) { return os.ReadFile(path) }
