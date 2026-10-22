package session

import (
	"flag"
	"fmt"
	"log/slog"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunRename changes a session's slug and waits for the daemon to confirm, so
// a rejection (slug already in use, bad format) reaches the user.
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
	if err := protocol.ValidateSlug(newSlug); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, "rename: "+err.Error())
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}
	reqID := "cli-rename-" + sess.ShortID
	slog.Debug("cli: rename", "session", sess.ID, "from", sess.Slug, "to", newSlug, "req_id", reqID)
	if err := c.Rename(sess.ID, newSlug, "", reqID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := awaitPatch(c, sess.ID, reqID, "slug"); err != nil {
		return err
	}
	fmt.Printf("%s: renamed to %s\n", sess.Slug, newSlug)
	return nil
}
