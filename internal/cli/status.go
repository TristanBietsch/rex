package cli

import (
	"flag"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunStatus prints an aggregate one-liner of session counts.
//
// Exit code is 1 when one or more sessions are in `needs_input`, 0 otherwise.
// Useful for shell prompts as an ambient indicator.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}

	if *asJSON {
		if err := WriteAggregateJSON(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	} else {
		if err := WriteAggregateLine(os.Stdout, snap.Sessions); err != nil {
			return err
		}
	}

	for _, s := range snap.Sessions {
		if s.State == protocol.StateNeedsInput {
			return NewExitError(ExitGeneric, "")
		}
	}
	return nil
}
