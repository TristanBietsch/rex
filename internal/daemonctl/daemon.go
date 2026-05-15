// Package daemonctl spawns, checks, and resolves the rex-daemon process.
// Shared between the CLI (rex daemon start) and the TUI boot splash.
package daemonctl

import (
	"fmt"
	"net"
	"os"
	"os/exec"
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

// FindBinary returns the path to rex-daemon. It checks (in order):
//   1) the same directory as the current rex executable,
//   2) PATH lookup,
//   3) the literal name "rex-daemon" (so exec gives a useful error).
func FindBinary() string {
	if self, err := os.Executable(); err == nil {
		if path := findBinaryIn(filepath.Dir(self)); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("rex-daemon"); err == nil {
		return path
	}
	return "rex-daemon"
}

// findBinaryIn is FindBinary's first probe, exposed for tests.
func findBinaryIn(dir string) string {
	candidate := filepath.Join(dir, "rex-daemon")
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

// StartResult describes a successful daemon spawn.
type StartResult struct {
	PID     int
	Elapsed time.Duration
}

// Start spawns rex-daemon and waits up to ~2 s for the socket to appear.
// Returns nil StartResult on failure. The caller chooses the log file (pass nil
// to discard stderr).
func Start(socket string, stderrLog *os.File) (*StartResult, error) {
	t0 := time.Now()
	cmd := exec.Command(FindBinary())
	if stderrLog != nil {
		cmd.Stderr = stderrLog
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn rex-daemon: %w", err)
	}
	for i := 0; i < 100; i++ {
		if Reachable(socket) {
			return &StartResult{PID: cmd.Process.Pid, Elapsed: time.Since(t0)}, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon started but socket %s didn't appear within 2s", socket)
}
