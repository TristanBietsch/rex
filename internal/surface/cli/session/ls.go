package session

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunLs prints the session list in the requested output format.
func RunLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats, core.WithLegacyJSON())
	stateFilter := fs.String("state", "", "filter by state")
	toolFilter := fs.String("tool", "", "filter by tool id")
	modelFilter := fs.String("model", "", "filter by model id")
	showArchived := fs.Bool("show-archived", false, "(deprecated, no effect)")
	short := fs.Bool("short", false, "(deprecated, no effect)")
	wide := fs.Bool("wide", false, "add STARTED and ENDED columns")
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	_ = showArchived
	_ = short

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.HelloFailed(err)
	}

	filtered := make([]protocol.SessionSummary, 0, len(snap.Sessions))
	for _, s := range snap.Sessions {
		if *stateFilter != "" && string(s.State) != *stateFilter {
			continue
		}
		if *toolFilter != "" && s.ToolID != *toolFilter {
			continue
		}
		if *modelFilter != "" && s.ModelID != *modelFilter {
			continue
		}
		filtered = append(filtered, s)
	}

	p := core.Out()
	return p.Emit(core.Result{
		Cmd:    "ls",
		Data:   filtered,
		View:   lsView(filtered, p.Now(), p.Glyphs, lsOpts{Wide: *wide, Times: *wide && featureOn("timestamps"), Resume: *wide && featureOn("resume")}),
		Legacy: func(w io.Writer) error { return core.WriteSessionsJSONL(w, filtered) },
	})
}

// lsOpts selects the extra `-wide` columns.
type lsOpts struct {
	// Wide is -wide.
	Wide bool
	// Times adds STARTED and ENDED (the timestamps feature).
	Times bool
	// Resume adds RESUME ID (the resume feature).
	Resume bool
}

// clockTime is t in now's zone as "2006-01-02 15:04", or "-" when unset.
func clockTime(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(now.Location()).Format("2006-01-02 15:04")
}

// endedAt is when the session reached a terminal state. An older daemon sends
// no ended_at, so a finished session falls back to its last event.
func endedAt(s protocol.SessionSummary) time.Time {
	if !s.EndedAt.IsZero() {
		return s.EndedAt
	}
	switch s.State {
	case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
		return s.LastEventAt
	case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
	}
	return time.Time{}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// stateLabel is the human name of a session state.
func stateLabel(s protocol.State) string {
	if s == protocol.StateNeedsInput {
		return "needs you"
	}
	return string(s)
}

func stateTone(s protocol.State) core.Tone {
	switch s {
	case protocol.StateNeedsInput:
		return core.ToneNeeds
	case protocol.StateWorking:
		return core.ToneWorking
	case protocol.StateQueued:
		return core.ToneQueued
	case protocol.StateDone:
		return core.ToneDone
	case protocol.StateFailed:
		return core.ToneFailed
	case protocol.StateCrashed:
		return core.ToneCrashed
	}
	return core.ToneNone
}

// lsView builds the session list. The txt columns reproduce `rex ls` as it
// always printed (ID STATE TOOL MODEL SLUG LAST EVENT NAME, NAME last); text
// swaps in a state marker and a compact IDLE column (and moves NAME to a
// second line on a narrow terminal); md and agent lines carry the real name,
// without the done marker, and agent lines model and effort apart and idle_s
// in seconds (-1 when unknown).
func lsView(sessions []protocol.SessionSummary, now time.Time, g core.Glyphs, opts lsOpts) core.View {
	text, txt, md := core.SetOf(core.FormatText), core.SetOf(core.FormatTXT), core.SetOf(core.FormatMD)
	agent := core.SetOf(core.FormatAgent)
	human := text | md
	cols := []core.Col{
		{Key: "mark", Title: "", Formats: text},
		{Key: "id", Title: "ID", Width: 5},
		{Key: "state", Title: "State", Width: 12, Flex: true, Min: 6},
		{Key: "tool", Title: "Tool", Width: 9, DropBelow: 60},
		{Key: "model", Title: "Model", Width: 20, Max: 20, Flex: true, Formats: human | txt},
		{Key: "model", Title: "Model", Formats: agent},
		{Key: "effort", Title: "Effort", Formats: agent},
		{Key: "slug", Title: "Slug", Width: 24, Max: 24, Flex: true},
		{Key: "last_event", Title: "Last event", Width: 10, Formats: txt},
		{Key: "idle", Title: "Idle", Align: core.AlignRight, Formats: human},
		{Key: "idle_s", Title: "Idle s", Formats: agent},
	}
	if opts.Wide && opts.Times {
		// Before NAME, which stays last in txt.
		cols = append(cols,
			core.Col{Key: "started", Title: "Started", DropBelow: 100},
			core.Col{Key: "ended", Title: "Ended", DropBelow: 100})
	}
	if opts.Wide && opts.Resume {
		cols = append(cols, core.Col{Key: "resume_id", Title: "Resume ID", DropBelow: 140})
	}
	cols = append(cols,
		// text and txt show the done marker; md, agent and json carry the real name.
		core.Col{Key: "name", Title: "Name", Below: true, Formats: text | txt},
		core.Col{Key: "name", Title: "Name", Formats: md | agent})
	var needs, working, done int
	rows := make([][]core.Cell, 0, len(sessions))
	for _, s := range sessions {
		switch s.State {
		case protocol.StateNeedsInput:
			needs++
		case protocol.StateWorking:
			working++
		case protocol.StateDone:
			done++
		case protocol.StateQueued, protocol.StateFailed, protocol.StateCrashed:
		}
		model := s.ModelID
		if s.Effort != "" {
			model += " · " + s.Effort
		}
		shown := s.Name
		if !s.RetireAt.IsZero() {
			shown = strings.TrimSpace(shown + " ✓done")
		}
		ago := core.DurationAgoAt(s.LastEventAt, now)
		idleS := -1
		if !s.LastEventAt.IsZero() {
			idleS = int(now.Sub(s.LastEventAt).Seconds())
		}
		tone := stateTone(s.State)
		cells := []core.Cell{
			{Text: g.State[s.State], Tone: tone},
			{Text: s.ShortID},
			{Text: stateLabel(s.State), Txt: string(s.State), Raw: string(s.State), Tone: tone},
			{Text: s.ToolID},
			{Text: model},
			{Text: s.ModelID},
			{Text: s.Effort},
			{Text: s.Slug},
			{Text: ago},
			{Text: strings.TrimSuffix(ago, " ago"), Tone: core.ToneDim},
			{Text: fmt.Sprint(idleS), Raw: idleS},
		}
		if opts.Wide && opts.Times {
			end := endedAt(s)
			cells = append(cells,
				core.Cell{Text: clockTime(now, s.StartedAt), Raw: rfc3339(s.StartedAt)},
				core.Cell{Text: clockTime(now, end), Raw: rfc3339(end)})
		}
		if opts.Wide && opts.Resume {
			id := "-"
			if protocol.ValidResumeID(s.ResumeID) {
				id = s.ResumeID
			}
			raw := id
			if id == "-" {
				raw = ""
			}
			cells = append(cells, core.Cell{Text: id, Raw: raw})
		}
		cells = append(cells, core.Cell{Text: shown}, core.Cell{Text: s.Name})
		rows = append(rows, cells)
	}
	meta := []string{}
	for _, c := range []struct {
		n     int
		label string
	}{{needs, "needs you"}, {working, "working"}, {done, "done"}} {
		if c.n > 0 {
			meta = append(meta, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	v := core.View{
		core.Header{Title: "Sessions", Meta: strings.Join(meta, " · ")},
		core.Table{Kind: "session", Cols: cols, Rows: rows, Empty: "(no sessions)"},
	}
	if len(sessions) > 0 {
		v = append(v, core.Hint{Cmd: "rex attach <id>", Text: "open a session"})
	}
	return v
}
