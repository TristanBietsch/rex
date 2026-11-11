package lifecycle

import (
	"errors"
	"flag"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunReload asks the rex-daemon on the default socket to re-read tools.yaml
// with a Reload intent. It never signals a process; nothing answering on
// the socket exits ExitDaemonUnreachable.
func RunReload(args []string) error {
	fs := flag.NewFlagSet("reload", flag.ContinueOnError)
	_, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := daemonctl.Reload(); err != nil {
		if errors.Is(err, daemonctl.ErrNotRunning) {
			return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
		}
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
