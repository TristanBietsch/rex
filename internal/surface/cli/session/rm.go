package session

import (
	"flag"
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// RunRm deletes a session.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	force := fs.Bool("force", false, "skip confirmation when stdin is a TTY")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "rm: exactly one selector required")
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

	fi, _ := os.Stdin.Stat()
	if !*force && (fi.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintf(os.Stderr, "delete %s (%s)? [y/N] ", sess.ShortID, sess.Slug)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if ans != "y" && ans != "Y" && ans != "yes" {
			return core.NewExitError(core.ExitGeneric, "aborted")
		}
	}

	if err := c.Delete(sess.ID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
