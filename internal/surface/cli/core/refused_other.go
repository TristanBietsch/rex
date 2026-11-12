//go:build !windows

package core

import (
	"errors"
	"syscall"
)

// connRefused reports a dial that found no listener (a socket file a dead
// daemon left behind).
func connRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
