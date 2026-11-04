//go:build !windows

package state

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes a non-blocking exclusive flock on f. flock locks belong to
// the open file description, so a second open of the same file (even in
// this process) conflicts, and the kernel drops the lock when the process
// exits.
func lockFile(f *os.File) error {
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
