package core

import "fmt"

// ActionView is the view of an action's result: one confirmation Line for
// the human formats, and fields for the agent "ok" line.
//
//	core.ActionView(core.Line{Tone: core.ToneOK, Glyph: g.OK, Text: "resumed 7d4f fix-auth",
//		Txt: "fix-auth: resuming"},
//		core.Field{Key: "id", Val: "7d4f"}, core.Field{Key: "outcome", Val: "resuming"})
//
// The fields appear only in agent mode (in the trailer, in the order given);
// json carries the typed Result.Data instead.
func ActionView(l Line, fields ...Field) View {
	v := View{l}
	if len(fields) == 0 {
		return v
	}
	pairs := make([]Pair, len(fields))
	for i, f := range fields {
		pairs[i] = Pair{Key: f.Key, Val: Cell{Text: fmt.Sprint(f.Val), Raw: f.Val}, Formats: SetOf(FormatAgent)}
	}
	return append(v, KV{Pairs: pairs})
}

// OffMessage is the refusal a CLI command gives while its Features switch is
// off.
func OffMessage(id string) string {
	return "off: enable in Settings → Features (rex config set " + id + " true)"
}

// OffError is OffMessage as an ExitInvalidArgs error (exit 5), hinting the
// command that switches the feature back on.
func OffError(id string) ExitError {
	return Errorf(ExitInvalidArgs, "rex config set "+id+" true", "%s", OffMessage(id))
}
