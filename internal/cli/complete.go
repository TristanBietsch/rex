package cli

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunComplete cleanly terminates a running session and marks it done.
// Distinct from rm (which deletes) and archive (which renames).
func RunComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "complete: exactly one selector required")
	}
	sel := fs.Arg(0)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Complete(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
