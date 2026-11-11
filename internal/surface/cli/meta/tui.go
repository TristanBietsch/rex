package meta

import (
	"log/slog"
	"os"

	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// QuietBootEnv set to a non-empty value other than "0" silences the boot
// splash sounds, like --quiet-boot.
const QuietBootEnv = "REX_QUIET_BOOT"

// RunTUI opens the Bubble Tea board (no-args entry). quietBoot (or
// REX_QUIET_BOOT) skips the boot sounds, for scripted relaunches; a plain
// `rex` from a terminal keeps them.
func RunTUI(quietBoot bool) error {
	if v := os.Getenv(QuietBootEnv); v != "" && v != "0" {
		quietBoot = true
	}
	rexlog.Init("tui")
	defer rexlog.Close()
	socket, err := core.DefaultSocket()
	if err != nil {
		return err
	}
	slog.Info("tui: starting", "socket", socket, "quiet_boot", quietBoot)
	err = tui.Run(socket, quietBoot)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
}
