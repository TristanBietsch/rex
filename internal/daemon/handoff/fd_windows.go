//go:build windows

package handoff

import (
	"errors"
	"io/fs"
)

// Supported reports whether this platform can hot restart. Windows has no
// execve: rex daemon restart falls back to a cold restart.
const Supported = false

// ErrUnsupported is returned by Inherit and Exec on Windows.
var ErrUnsupported = errors.New("hot restart is not supported on Windows")

// Inherit is not supported on Windows.
func Inherit(...int) (func(), error) { return func() {}, ErrUnsupported }

// Exec is not supported on Windows.
func Exec(string, []string, []string) error { return ErrUnsupported }

func checkOwner(fs.FileInfo) error { return ErrUnsupported }
