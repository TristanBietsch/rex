package cli

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunTUI opens the Bubble Tea board (no-args entry).
func RunTUI() error {
	rexlog.Init("tui")
	defer rexlog.Close()
	socket := DefaultSocket()
	slog.Info("tui: starting", "socket", socket)
	err := tui.Run(socket)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
}
