package cli

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunRename changes a session's slug.
func RunRename(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 2 {
		return NewExitError(ExitInvalidArgs, "rename: <selector> <new-slug>")
	}
	sel := fs.Arg(0)
	newSlug := fs.Arg(1)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	if err := c.Rename(sess.ID, newSlug, ""); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
