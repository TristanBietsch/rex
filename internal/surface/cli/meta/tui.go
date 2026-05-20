package meta

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// RunTUI opens the Bubble Tea board (no-args entry).
func RunTUI() error {
	rexlog.Init("tui")
	defer rexlog.Close()
	socket := core.DefaultSocket()
	slog.Info("tui: starting", "socket", socket)
	err := tui.Run(socket)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
}
