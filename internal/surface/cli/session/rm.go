package session

import (
	"flag"
	"fmt"
	"os"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunRm deletes a session.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	force := fs.Bool("force", false, "skip confirmation when stdin is a TTY")
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "rm: exactly one selector required")
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
