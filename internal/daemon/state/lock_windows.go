//go:build windows

package state

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// lockRangeOffsetHigh places the locked byte at offset 4 GiB, past any pid
// the file holds. LockFileEx locks are mandatory for the locked range, so
// locking byte 0 would stop the second daemon from reading the pid it
// needs for its error message.
const lockRangeOffsetHigh = 1

// lockFile takes a non-blocking exclusive LockFileEx lock on f. Locks belong
// to the handle, so a second open of the same file (even in this process)
// conflicts, and Windows drops the lock when the handle or process goes away.
func lockFile(f *os.File) error {
	ol := windows.Overlapped{OffsetHigh: lockRangeOffsetHigh}
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errWouldBlock
	}
	if err != nil {
		return fmt.Errorf("LockFileEx: %w", err)
	}
	return nil
}

func unlockFile(f *os.File) error {
	ol := windows.Overlapped{OffsetHigh: lockRangeOffsetHigh}
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol); err != nil {
		return fmt.Errorf("UnlockFileEx: %w", err)
	}
	return nil
}
