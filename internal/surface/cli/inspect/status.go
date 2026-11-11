package inspect

import (
	"flag"
	"fmt"
	"io"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunStatus prints an aggregate summary of session counts.
//
// Exit code is 1 when one or more sessions are in `needs_input`, 0 otherwise.
// Useful for shell prompts as an ambient indicator.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}

	counts := countSessions(snap.Sessions)
	p := core.Out()
	if err := p.Emit(core.Result{
		Cmd:  "status",
		Data: counts,
		View: statusView(counts, snap.Sessions, p.Glyphs),
		Legacy: func(w io.Writer) error {
			return core.WriteAggregateJSON(w, snap.Sessions)
		},
	}); err != nil {
		return err
	}

	if counts.NeedsInput > 0 {
		return core.NewExitError(core.ExitGeneric, "")
	}
	return nil
}

// statusCounts is the `-o json` payload of rex status. Failed includes
// crashed sessions.
type statusCounts struct {
	NeedsInput int `json:"needs_input"`
	Working    int `json:"working"`
	Queued     int `json:"queued"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
}

func countSessions(sessions []protocol.SessionSummary) statusCounts {
	var c statusCounts
	for _, s := range sessions {
		switch s.State {
		case protocol.StateNeedsInput:
			c.NeedsInput++
		case protocol.StateWorking:
			c.Working++
		case protocol.StateQueued:
			c.Queued++
		case protocol.StateDone:
			c.Done++
		case protocol.StateFailed, protocol.StateCrashed:
			c.Failed++
		}
	}
	return c
}

// statusView builds the one-line summary. Text shows "◆ 1 needs you   ⠋ 1
// working   ● 2 done" (queued and failed only when non-zero); txt is the
// exact line shell prompts have always parsed; agent gets the full set of
// counts on the ok line.
func statusView(c statusCounts, sessions []protocol.SessionSummary, g core.Glyphs) core.View {
	always := core.FormatSet(0)
	showIf := func(n int) core.FormatSet {
		if n > 0 {
			return always
		}
		return core.SetOf(core.FormatAgent)
	}
	pair := func(key, label string, n int, st protocol.State, tone core.Tone, f core.FormatSet) core.Pair {
		return core.Pair{Key: key, Label: label, Glyph: g.State[st], Formats: f,
			Val: core.Cell{Text: fmt.Sprint(n), Raw: n, Tone: tone}}
	}
	return core.View{core.KV{
		Inline: true,
		Txt:    core.AggregateLine(sessions),
		Pairs: []core.Pair{
			pair("needs_input", "needs you", c.NeedsInput, protocol.StateNeedsInput, core.ToneNeeds, always),
			pair("working", "working", c.Working, protocol.StateWorking, core.ToneWorking, always),
			pair("queued", "queued", c.Queued, protocol.StateQueued, core.ToneQueued, showIf(c.Queued)),
			pair("done", "done", c.Done, protocol.StateDone, core.ToneDone, always),
			pair("failed", "failed", c.Failed, protocol.StateFailed, core.ToneFailed, showIf(c.Failed)),
		},
	}}
}
