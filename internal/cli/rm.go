package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/client"
)

// RunRm deletes a session.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	force := fs.Bool("force", false, "skip confirmation when stdin is a TTY")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "rm: exactly one selector required")
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

	fi, _ := os.Stdin.Stat()
	if !*force && (fi.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintf(os.Stderr, "delete %s (%s)? [y/N] ", sess.ShortID, sess.Slug)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if ans != "y" && ans != "Y" && ans != "yes" {
			return NewExitError(ExitGeneric, "aborted")
		}
	}

	if err := c.Delete(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	return nil
}
