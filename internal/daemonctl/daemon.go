// Package daemonctl spawns, checks, and resolves the rex-daemon process.
// Shared between the CLI (rex daemon start) and the TUI boot splash.
package daemonctl

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

// Reachable returns true when a unix socket connect succeeds quickly.
func Reachable(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DefaultSocket returns the default UDS path, matching rex-daemon's logic.
func DefaultSocket() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}
