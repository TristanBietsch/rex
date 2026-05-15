package cli

import (
	"log/slog"
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunTUI opens the Bubble Tea board (no-args entry).
// If the daemon isn't running, start it first.
func RunTUI() error {
	rexlog.Init("tui")
	defer rexlog.Close()
	socket := DefaultSocket()
	if !daemonReachable(socket) {
		slog.Info("tui: daemon not reachable, starting", "socket", socket)
		if err := daemonStart(nil); err != nil {
			slog.Error("tui: daemon start failed", "err", err)
			return err
		}
	}
	slog.Info("tui: starting", "socket", socket)
	err := tui.Run(socket)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
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
