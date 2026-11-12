//go:build windows

package core

import (
	"errors"

	"golang.org/x/sys/windows"
)

// connRefused reports a dial that found no listener. Windows reports a
// missing or stale AF_UNIX socket as WSAECONNREFUSED, which errors.Is does
// not match against syscall.ECONNREFUSED.
func connRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED)
}
