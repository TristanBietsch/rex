//go:build windows

package pty

import (
	"errors"
	"time"
)

// canWaitReadable is false on Windows: there is no hot restart, so the
// reader blocks in Read as before.
func canWaitReadable(int) bool { return false }

func waitReadable(int, time.Duration) (bool, error) {
	return false, errors.New("waitReadable: not supported on Windows")
}
