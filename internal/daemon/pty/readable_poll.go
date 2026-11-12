//go:build !windows && !darwin

package pty

import (
	"time"

	"golang.org/x/sys/unix"
)

// canWaitReadable reports whether waitReadable works for fd.
func canWaitReadable(fd int) bool { return fd >= 0 }

// waitReadable waits up to d for fd to be readable (data, EOF or hangup).
// A false result with a nil error is a timeout or an interrupted wait.
func waitReadable(fd int, d time.Duration) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // G115: descriptors fit an int32.
	n, err := unix.Poll(fds, int(d.Milliseconds()))
	if err != nil {
		if err == unix.EINTR {
			return false, nil
		}
		return false, err
	}
	return n > 0 && fds[0].Revents != 0, nil
}
