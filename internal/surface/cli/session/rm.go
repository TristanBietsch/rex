package session

import (
	"flag"
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunRm deletes a session. Without a terminal it needs -force, like prune
// needs -yes: a script never deletes without saying so.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	var force bool
	fs.BoolVar(&force, "force", false, "do not ask (required when stdin is not a terminal)")
	fs.BoolVar(&force, "yes", false, "alias for -force")
	fs.BoolVar(&force, "y", false, "alias for -force")
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

	if !force {
		if !pruneIO.interactive() {
			return core.Errorf(core.ExitOperationRefused, "rex rm -force "+sel,
				"rm: stdin is not a terminal; pass -force to delete without asking")
		}
		fmt.Fprintf(pruneIO.errOut, "delete %s (%s)? [y/N] ", sess.ShortID, sess.Slug)
		var ans string
		_, _ = fmt.Fscanln(pruneIO.in, &ans)
		if a := strings.ToLower(ans); a != "y" && a != "yes" {
			return core.Errorf(core.ExitOperationRefused, "", "rm: not confirmed, nothing deleted")
		}
	}

	if err := c.Delete(sess.ID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
