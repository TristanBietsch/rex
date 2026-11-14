//go:build windows

package workspace

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile takes a non-blocking exclusive LockFileEx lock on the first
// byte of f (as the daemon's state lock does). Locks belong to the handle,
// so another process, or another open in this one, conflicts, and Windows
// drops the lock when the handle or process goes away.
func tryLockFile(f *os.File) error {
	ol := windows.Overlapped{}
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
	ol := windows.Overlapped{}
	if err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol); err != nil {
		return fmt.Errorf("UnlockFileEx: %w", err)
	}
	return nil
}
