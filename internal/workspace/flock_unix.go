//go:build !windows

package workspace

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile takes a non-blocking exclusive flock on f (as the daemon's
// state lock does). flock locks belong to the open file description, so
// another process, or another open in this one, conflicts, and the kernel
// drops the lock when the process exits.
func tryLockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errWouldBlock
	}
	if err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
