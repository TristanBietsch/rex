package session

import (
	"flag"
	"fmt"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunArchive marks a completed session as archived by prefixing its Title.
func RunArchive(args []string) error {
	fs := flag.NewFlagSet("archive", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	rest, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "archive: exactly one selector required")
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
	newTitle := "[archived] " + sess.Title
	reqID := "cli-archive-" + sess.ShortID
	if err := c.Rename(sess.ID, "", newTitle, reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "title"); err != nil {
		return err
	}
	fmt.Printf("%s: archived\n", sess.Slug)
	return nil
}
