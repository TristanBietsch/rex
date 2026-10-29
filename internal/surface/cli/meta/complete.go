package meta

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunComplete cleanly terminates a running session and marks it done.
// Distinct from rm (which deletes) and archive (which renames).
func RunComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex complete <sel> [-socket path]"); err != nil {
		return err
	}
	sel := pos[0]

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
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
