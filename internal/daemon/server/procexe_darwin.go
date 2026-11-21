//go:build darwin

package server

import (
	"bytes"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// procExe returns the executable path of process pid as the kernel recorded
// it at exec (sysctl kern.procargs2). The process set that string itself, so
// it may be relative or go through a symlink: it is reported, not verified.
func procExe(pid int) (string, error) {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", fmt.Errorf("process %d: kern.procargs2: %w", pid, err)
	}
	exe, err := parseProcArgsExe(buf)
	if err != nil {
		return "", fmt.Errorf("process %d: %w", pid, err)
	}
	return exe, nil
}

// parseProcArgsExe extracts the executable path from a kern.procargs2
// buffer: a 4-byte argc, then the path, NUL-terminated. A short buffer, a
// path with no terminating NUL or an empty path is an error.
func parseProcArgsExe(buf []byte) (string, error) {
	const argcLen = 4
	if len(buf) <= argcLen {
		return "", errors.New("kern.procargs2: buffer too short")
	}
	rest := buf[argcLen:]
	end := bytes.IndexByte(rest, 0)
	switch {
	case end < 0:
		return "", errors.New("kern.procargs2: executable path not terminated")
	case end == 0:
		return "", errors.New("kern.procargs2: empty executable path")
	}
	return string(rest[:end]), nil
}
