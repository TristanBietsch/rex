package session

import (
	"flag"
	"fmt"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunRm deletes a session. Without a terminal it needs -force, like prune
// needs -yes: a script never deletes without saying so.
func RunRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	var force bool
	fs.BoolVar(&force, "force", false, "do not ask (required when stdin is not a terminal)")
	fs.BoolVar(&force, "yes", false, "alias for -force")
	fs.BoolVar(&force, "y", false, "alias for -force")
	rest, err := out.Parse(fs, args)
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
		ok, err := core.Out().Confirm(fmt.Sprintf("delete %s (%s)?", sess.ShortID, sess.Slug), core.Refusal{
			Cmd: "rm", Flag: "-force", What: "delete", Hint: "rex rm " + sel + " -force"})
		if err != nil {
			return err
		}
		if !ok {
			return core.Errorf(core.ExitOperationRefused, "", "rm: not confirmed, nothing deleted")
		}
	}

	if err := c.Delete(sess.ID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return emitAction("rm", refOf(sess), "deleted "+sess.ShortID+" "+sess.Slug, "", sessionFields(sess))
}
