//go:build linux || darwin

package server

import (
	"fmt"
	"net"
)

// controlFD runs fn with the connection's file descriptor.
func controlFD(c *net.UnixConn, fn func(fd int) error) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return fmt.Errorf("syscall conn: %w", err)
	}
	var ferr error
	if err := raw.Control(func(fd uintptr) { ferr = fn(int(fd)) }); err != nil {
		return fmt.Errorf("control: %w", err)
	}
	return ferr
}
