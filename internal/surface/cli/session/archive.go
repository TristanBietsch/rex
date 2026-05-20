package session

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunArchive marks a completed session as archived by prefixing its Title.
func RunArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "archive: exactly one selector required")
	}
	sel := fs.Arg(0)

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	newTitle := "[archived] " + sess.Title
	if err := c.Rename(sess.ID, "", newTitle); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
