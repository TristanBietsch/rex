package lifecycle

import (
	"errors"
	"flag"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunReload sends SIGHUP to the running rex-daemon so the user/script can
// trigger a config re-read.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if err := daemonctl.Reload(); err != nil {
		if errors.Is(err, daemonctl.ErrNotRunning) {
			return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
		}
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
