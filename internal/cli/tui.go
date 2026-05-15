package cli

import (
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/tui"
)

// RunTUI opens the Bubble Tea board (no-args entry).
// If the daemon isn't running, start it first.
func RunTUI() error {
	socket := DefaultSocket()
	if !daemonReachable(socket) {
		if err := daemonStart(nil); err != nil {
			return err
		}
	}
	return tui.Run(socket)
}

// daemonReachable returns true if a unix socket connect succeeds quickly.
func daemonReachable(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
