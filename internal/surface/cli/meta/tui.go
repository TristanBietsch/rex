package meta

import (
	"errors"
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
func RunTUI(quietBoot bool) error { return RunTUIScoped(quietBoot, nil) }

// RunTUIScoped is RunTUI for a board fixed to one workspace (rex here); a nil
// scope is the ordinary board.
func RunTUIScoped(quietBoot bool, scope *tui.WorkspaceScope) error {
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
	opts := tui.RunOptions{QuietBoot: quietBoot, Scope: scope, SwapState: os.Getenv(tui.SwapEnv)}
	if opts.SwapState != "" {
		_ = os.Unsetenv(tui.SwapEnv)
		slog.Info("tui: started by a hot swap")
	}
	err = swapLoop(opts, func(o tui.RunOptions) error { return tui.RunWith(socket, o) }, execSwap)
	if err != nil {
		slog.Error("tui: exited with error", "err", err)
	} else {
		slog.Info("tui: exited cleanly")
	}
	return err
}

// swapLoop runs the board; when it quits to swap, it execs the installed
// binary (exec returns only on failure) and otherwise runs the board again
// in this build with the same view and a notice.
func swapLoop(opts tui.RunOptions, run func(tui.RunOptions) error, exec func(string) error) error {
	for {
		err := run(opts)
		var swap *tui.SwapRequest
		if !errors.As(err, &swap) {
			return err
		}
		execErr := exec(swap.Path)
		slog.Error("tui: hot swap failed; staying on this build", "err", execErr)
		opts = tui.RunOptions{QuietBoot: true, Scope: opts.Scope, SwapState: swap.Path,
			Notice: "swap failed: " + execErr.Error()}
	}
}
