package session

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunRename changes a session's slug.
func RunRename(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 2 {
		return core.NewExitError(core.ExitInvalidArgs, "rename: <selector> <new-slug>")
	}
	sel := fs.Arg(0)
	newSlug := fs.Arg(1)

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Rename(sess.ID, newSlug, ""); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
