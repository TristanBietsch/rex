package inspect

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunStatus prints an aggregate one-liner of session counts.
//
// Exit code is 1 when one or more sessions are in `needs_input`, 0 otherwise.
// Useful for shell prompts as an ambient indicator.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	asJSON := fs.Bool("json", false, "output JSON")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 0, 0, "rex status [-json] [-socket path]"); err != nil {
		return err
	}

	c, snap, err := core.Dial(*socket)
	if err != nil {
		return err
	}
	defer c.Close()

	if *asJSON {
		if err := core.WriteAggregateJSON(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	} else {
		if err := core.WriteAggregateLine(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	}

	for _, s := range snap.Sessions {
		if s.State == protocol.StateNeedsInput {
			return core.NewExitError(core.ExitGeneric, "")
		}
	}
	return nil
}
