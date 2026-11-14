package session

import (
	"flag"
	"fmt"
	"log/slog"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunRename changes a session's slug and waits for the daemon to confirm, so
// a rejection (slug already in use, bad format) reaches the user.
func RunRename(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return core.NewExitError(core.ExitInvalidArgs, "rename: <selector> <new-slug>")
	}
	sel := rest[0]
	newSlug := rest[1]
	if err := protocol.ValidateSlug(newSlug); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, "rename: "+err.Error())
	}

	c, err := socket.Dial()
	if err != nil {
		return err
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
	renamed := sess
	renamed.Slug = newSlug
	return emitAction("rename", renameData{refOf(renamed), sess.Slug}, "renamed "+sess.Slug+" to "+newSlug,
		fmt.Sprintf("%s: renamed to %s", sess.Slug, newSlug),
		sessionFields(renamed, core.Field{Key: "old_slug", Val: sess.Slug}))
}

// renameData is the `-o json` payload of rex rename; the slug is the new one.
type renameData struct {
	actionData
	OldSlug string `json:"old_slug"`
}
