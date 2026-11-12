//go:build !windows

package handoff

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Supported reports whether this platform can hot restart.
const Supported = true

// Inherit clears FD_CLOEXEC on each fd so it stays open across execve.
// restore sets it back (the exec failed and the daemon keeps running).
func Inherit(fds ...int) (restore func(), err error) {
	var done []int
	restore = func() {
		for _, fd := range done {
			_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC)
		}
	}
	for _, fd := range fds {
		if fd < 0 {
			restore()
			return func() {}, fmt.Errorf("handoff: bad fd %d", fd)
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			restore()
			return func() {}, fmt.Errorf("handoff: fd %d: %w", fd, err)
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags&^unix.FD_CLOEXEC); err != nil {
			restore()
			return func() {}, fmt.Errorf("handoff: fd %d: %w", fd, err)
		}
		done = append(done, fd)
	}
	return restore, nil
}

// Exec replaces this process image with bin (same pid).
func Exec(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env) //nolint:gosec // G204: bin is this daemon's own executable.
}

// checkOwner refuses a file another user owns or that group/other can use.
func checkOwner(fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("handoff: cannot read file owner")
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("handoff: file owned by uid %d", st.Uid)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("handoff: file mode %v is open to other users", fi.Mode().Perm())
	}
	return nil
}
