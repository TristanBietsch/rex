//go:build darwin

package pty

import (
	"time"

	"golang.org/x/sys/unix"
)

// maxSelectFD is FD_SETSIZE: select cannot watch a higher descriptor.
const maxSelectFD = 1024

// canWaitReadable reports whether waitReadable works for fd. macOS poll(2)
// does not support devices, so select(2) is used, bounded by FD_SETSIZE.
func canWaitReadable(fd int) bool { return fd >= 0 && fd < maxSelectFD }

// waitReadable waits up to d for fd to be readable (data, EOF or hangup).
// A false result with a nil error is a timeout or an interrupted wait.
func waitReadable(fd int, d time.Duration) (bool, error) {
	var set unix.FdSet
	set.Set(fd)
	tv := unix.NsecToTimeval(d.Nanoseconds())
	if _, err := unix.Select(fd+1, &set, nil, nil, &tv); err != nil {
		if err == unix.EINTR {
			return false, nil
		}
		return false, err
	}
	return set.IsSet(fd), nil
}
