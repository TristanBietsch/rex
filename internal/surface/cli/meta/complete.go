package meta

import (
	"flag"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunComplete cleanly terminates a running session and marks it done.
// Distinct from rm (which deletes) and archive (which renames).
func RunComplete(args []string) error {
	fs := flag.NewFlagSet("complete", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "complete: exactly one selector required")
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
	if err := c.Complete(sess.ID); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	p := core.Out()
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: "completed " + sess.ShortID + " " + sess.Slug, Silent: true}
	return p.Emit(core.Result{
		Cmd:  "complete",
		Data: completeData{ID: sess.ID, ShortID: sess.ShortID, Slug: sess.Slug},
		View: core.ActionView(line, core.Field{Key: "id", Val: sess.ShortID}, core.Field{Key: "slug", Val: sess.Slug}),
	})
}

// completeData is the `-o json` payload of rex complete.
type completeData struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Slug    string `json:"slug"`
}
