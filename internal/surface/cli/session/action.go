package session

import (
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// actionData is the `-o json` payload of the commands that act on one
// session. Commands add fields by embedding it.
type actionData struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Slug    string `json:"slug"`
}

func refOf(s protocol.SessionSummary) actionData {
	return actionData{ID: s.ID, ShortID: s.ShortID, Slug: s.Slug}
}

// sessionFields are the agent fields every per-session action starts with.
func sessionFields(s protocol.SessionSummary, more ...core.Field) []core.Field {
	return append([]core.Field{{Key: "id", Val: s.ShortID}, {Key: "slug", Val: s.Slug}}, more...)
}

// emitAction renders an action's result: a ✓ line (txt prints txt, or
// nothing when txt is empty and the command has always been quiet there) and
// the agent fields.
func emitAction(cmd string, data any, text, txt string, fields []core.Field, extra ...core.Block) error {
	p := core.Out()
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: text, Txt: txt, Silent: txt == ""}
	return p.Emit(core.Result{Cmd: cmd, Data: data, View: append(core.ActionView(line, fields...), extra...)})
}
