package meta

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunComplete cleanly terminates a running session and marks it done.
// Distinct from rm (which deletes) and archive (which renames).
func RunComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "complete: exactly one selector required")
	}
	sel := rest[0]

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Complete(sess.ID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
