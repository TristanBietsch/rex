//go:build !windows

package pty

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// R20 review: reading the PTY's descriptor for a freeze must not switch it
// to blocking mode (os.File.Fd does), or Close no longer interrupts a read
// or write in flight.
func TestFdOfKeepsNonBlocking(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()
	fd, ok := fdOf(&unixConsole{File: r})
	require.True(t, ok)
	fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	require.NoError(t, err)
	require.NotZero(t, fl&unix.O_NONBLOCK, "fdOf made the descriptor blocking")
}
