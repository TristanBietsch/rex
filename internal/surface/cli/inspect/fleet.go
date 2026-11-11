package inspect

import (
	"flag"
	"fmt"
	"log/slog"
	"sort"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/cli/session"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RunFleet dispatches rex fleet subcommands.
//
// Subcommands:
//
//	ls                        list distinct fleets and session counts
//	set <selector> <fleet>    assign a session to a fleet
//	unset <selector>          clear a session's fleet
//	show <fleet>              list sessions in a fleet
func RunFleet(args []string) error {
	fs := flag.NewFlagSet("fleet", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	sub := rest
	if len(sub) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "fleet: subcommand required (ls|set|unset|show)")
	}
	sock, err := socket.Path()
	if err != nil {
		return err
	}

	switch sub[0] {
	case "ls":
		return runFleetLS(sock)
	case "set":
		if len(sub) < 3 {
			return core.NewExitError(core.ExitInvalidArgs, "fleet set: usage: fleet set <selector> <fleet>")
		}
		return runFleetSet(sock, sub[1], sub[2])
	case "unset":
		if len(sub) < 2 {
			return core.NewExitError(core.ExitInvalidArgs, "fleet unset: usage: fleet unset <selector>")
		}
		return runFleetUnset(sock, sub[1])
	case "show":
		if len(sub) < 2 {
			return core.NewExitError(core.ExitInvalidArgs, "fleet show: usage: fleet show <fleet>")
		}
		return runFleetShow(sock, sub[1])
	default:
		return core.NewExitError(core.ExitInvalidArgs, "fleet: unknown subcommand: "+sub[0])
	}
}

// fleetRow is one fleet in the `-o json` payload of rex fleet ls.
type fleetRow struct {
	Fleet    string `json:"fleet"`
	Sessions int    `json:"sessions"`
}

// fleetCounts counts sessions per fleet, "(none)" for unassigned, sorted by
// name.
func fleetCounts(sessions []protocol.SessionSummary) []fleetRow {
	counts := map[string]int{}
	for _, s := range sessions {
		key := s.Fleet
		if key == "" {
			key = "(none)"
		}
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]fleetRow, len(keys))
	for i, k := range keys {
		rows[i] = fleetRow{Fleet: k, Sessions: counts[k]}
	}
	return rows
}

func fleetListView(rows []fleetRow) core.View {
	cells := make([][]core.Cell, len(rows))
	for i, r := range rows {
		cells[i] = []core.Cell{{Text: r.Fleet}, {Text: fmt.Sprint(r.Sessions), Raw: r.Sessions}}
	}
	return core.View{core.Table{Kind: "fleet", Rows: cells, Cols: []core.Col{
		{Key: "fleet", Title: "Fleet", Width: 24},
		{Key: "sessions", Title: "Sessions"},
	}}}
}

func runFleetLS(socket string) error {
	c, err := core.DialPath(socket)
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.HelloFailed(err)
	}
	rows := fleetCounts(snap.Sessions)
	return core.Out().Emit(core.Result{Cmd: "fleet", Data: rows, View: fleetListView(rows)})
}

// fleetSetData is the `-o json` payload of fleet set and unset; Fleet is
// empty after an unset.
type fleetSetData struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Slug    string `json:"slug"`
	Fleet   string `json:"fleet"`
}

// setFleet resolves selector, then sets its fleet (empty clears it).
func setFleet(socket, selector, fleet string) (protocol.SessionSummary, error) {
	c, err := core.DialPath(socket)
	if err != nil {
		return protocol.SessionSummary{}, err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, selector)
	if err != nil {
		return sess, err
	}

	// Reconnect — ResolveSelector consumed Hello.
	c2, err := core.DialPath(socket)
	if err != nil {
		return sess, err
	}
	defer c2.Close()
	if _, err := c2.Hello("rex-cli"); err != nil {
		return sess, core.HelloFailed(err)
	}
	if err := c2.SetSessionFleet(sess.ID, fleet); err != nil {
		return sess, core.NewExitError(core.ExitGeneric, err.Error())
	}
	return sess, nil
}

func emitFleetSet(sess protocol.SessionSummary, fleet, text, txt string) error {
	p := core.Out()
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: text, Txt: txt}
	return p.Emit(core.Result{
		Cmd:  "fleet",
		Data: fleetSetData{ID: sess.ID, ShortID: sess.ShortID, Slug: sess.Slug, Fleet: fleet},
		View: core.ActionView(line, core.Field{Key: "id", Val: sess.ShortID}, core.Field{Key: "slug", Val: sess.Slug},
			core.Field{Key: "fleet", Val: fleet}),
	})
}

func runFleetSet(socket, selector, fleet string) error {
	sess, err := setFleet(socket, selector, fleet)
	if err != nil {
		return err
	}
	slog.Info("fleet: set", "session", sess.ID, "fleet", fleet)
	return emitFleetSet(sess, fleet, fmt.Sprintf("set fleet %q on %s %s", fleet, sess.ShortID, sess.Slug),
		fmt.Sprintf("set fleet %q on session %s (%s)", fleet, sess.ShortID, sess.Slug))
}

func runFleetUnset(socket, selector string) error {
	sess, err := setFleet(socket, selector, "")
	if err != nil {
		return err
	}
	slog.Info("fleet: unset", "session", sess.ID, "fleet", "")
	return emitFleetSet(sess, "", fmt.Sprintf("cleared fleet on %s %s", sess.ShortID, sess.Slug),
		fmt.Sprintf("cleared fleet on session %s (%s)", sess.ShortID, sess.Slug))
}

func runFleetShow(socket, fleet string) error {
	c, err := core.DialPath(socket)
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.HelloFailed(err)
	}

	result := make([]protocol.SessionSummary, 0, len(snap.Sessions))
	for _, s := range snap.Sessions {
		if s.Fleet == fleet {
			result = append(result, s)
		}
	}
	p := core.Out()
	if len(result) == 0 {
		return p.Emit(core.Result{Cmd: "fleet", Data: result,
			View: core.View{core.Line{Text: fmt.Sprintf("no sessions in fleet %q", fleet)}}})
	}
	return p.Emit(core.Result{Cmd: "fleet", Data: result, View: session.SessionsView(result, p.Now(), p.Glyphs)})
}
