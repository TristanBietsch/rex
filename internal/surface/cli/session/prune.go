package session

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// deleteTimeout bounds one Delete: the daemon kills the agent's process
// group and drains its output before it acknowledges.
const deleteTimeout = 15 * time.Second

// RunPrune deletes every session in the board's Completed column (done,
// failed, crashed, or marked done) except those whose agent is still
// working. It asks first (on stderr); -yes skips the question and is
// required without a terminal or in a non-text format, where declining or
// being unable to ask exits 6.
func RunPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	dryRun := fs.Bool("dry-run", false, "list the sessions that would be cleared; delete nothing")
	var yes bool
	fs.BoolVar(&yes, "yes", false, "do not ask (required when stdin is not a terminal)")
	fs.BoolVar(&yes, "y", false, "shorthand for -yes")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return core.NewExitError(core.ExitInvalidArgs, "prune: takes no arguments")
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.HelloFailed(err)
	}
	targets, kept := tui.PruneTargets(snap.Sessions)
	note := tui.PruneKeptNote(kept)
	p := core.Out()
	if len(targets) == 0 {
		return emitPruneNothing(p, kept, note)
	}
	if *dryRun {
		return emitPruneDryRun(p, targets, kept, note)
	}
	if !yes {
		ok, err := p.Confirm(fmt.Sprintf("clear %d completed session(s)? this kills their agents and removes their transcripts%s", len(targets), note),
			core.Refusal{Cmd: "prune", Flag: "-yes", What: "delete", Extra: "or -dry-run to list", Hint: "rex prune -yes"})
		if err != nil {
			return err
		}
		if !ok {
			return core.Errorf(core.ExitOperationRefused, "", "prune: not confirmed, nothing deleted")
		}
	}

	var r pruneResult
	for _, s := range targets {
		o, err := prunePrecheckedDelete(socket, c, s)
		r.add(o, err, s, p)
	}
	if err := emitPruneSummary(p, r, kept, note); err != nil {
		return err
	}
	if r.failed > 0 {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("prune: %d of %d failed", r.failed, len(targets)))
	}
	return nil
}

// pruneRow is one session in the dry-run list.
type pruneRow struct {
	ID      string         `json:"id"`
	ShortID string         `json:"short_id"`
	Slug    string         `json:"slug"`
	State   protocol.State `json:"state"`
}

// pruneDryData is the `-o json` payload of rex prune -dry-run.
type pruneDryData struct {
	DryRun      bool       `json:"dry_run"`
	WouldClear  []pruneRow `json:"would_clear"`
	KeptWorking int        `json:"kept_working"`
}

func emitPruneNothing(p *core.Printer, kept int, note string) error {
	line := core.Line{Text: "nothing to clear" + note}
	return p.Emit(core.Result{
		Cmd:  "prune",
		Data: pruneDryData{WouldClear: []pruneRow{}, KeptWorking: kept},
		View: core.ActionView(line, core.Field{Key: "cleared", Val: 0}, core.Field{Key: "kept_working", Val: kept}),
	})
}

func emitPruneDryRun(p *core.Printer, targets []protocol.SessionSummary, kept int, note string) error {
	rows := make([]pruneRow, len(targets))
	cells := make([][]core.Cell, len(targets))
	for i, s := range targets {
		rows[i] = pruneRow{ID: s.ID, ShortID: s.ShortID, Slug: s.Slug, State: s.State}
		cells[i] = []core.Cell{
			{Text: s.ShortID}, {Text: s.Slug},
			{Text: core.StateLabel(s.State), Txt: string(s.State), Raw: string(s.State), Tone: core.StateTone(s.State)},
		}
	}
	foot := fmt.Sprintf("would clear %d%s", len(targets), note)
	agent := core.SetOf(core.FormatAgent)
	return p.Emit(core.Result{
		Cmd:  "prune",
		Data: pruneDryData{DryRun: true, WouldClear: rows, KeptWorking: kept},
		View: core.View{
			core.Table{Kind: "session", TxtRecords: true, TxtFoot: foot, Rows: cells, Cols: []core.Col{
				{Key: "id", Title: "ID"}, {Key: "slug", Title: "Slug"}, {Key: "state", Title: "State"}}},
			core.Note{Text: foot},
			core.KV{Pairs: []core.Pair{
				{Key: "dry_run", Val: core.Cell{Text: "true", Raw: true}, Formats: agent},
				{Key: "kept_working", Val: core.Cell{Text: fmt.Sprint(kept), Raw: kept}, Formats: agent},
			}},
		},
	})
}

// pruneData is the `-o json` payload of a rex prune that deleted.
type pruneData struct {
	Cleared     int `json:"cleared"`
	Unconfirmed int `json:"unconfirmed"`
	KeptChanged int `json:"kept_changed"`
	Failed      int `json:"failed"`
	KeptWorking int `json:"kept_working"`
}

func emitPruneSummary(p *core.Printer, r pruneResult, kept int, note string) error {
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: r.summary() + note, Txt: r.summary() + note}
	return p.Emit(core.Result{
		Cmd:  "prune",
		Data: pruneData{Cleared: r.cleared, Unconfirmed: r.unconfirmed, KeptChanged: r.skipped, Failed: r.failed, KeptWorking: kept},
		View: core.ActionView(line,
			core.Field{Key: "cleared", Val: r.cleared}, core.Field{Key: "unconfirmed", Val: r.unconfirmed},
			core.Field{Key: "kept_changed", Val: r.skipped}, core.Field{Key: "failed", Val: r.failed},
			core.Field{Key: "kept_working", Val: kept}),
	})
}

// pruneOutcome is what happened to one target.
type pruneOutcome int

const (
	pruneCleared     pruneOutcome = iota // the daemon acknowledged the delete, or it was already gone
	pruneUnconfirmed                     // sent to a daemon that predates acks
	pruneSkipped                         // no longer prunable (e.g. typed into, so working again)
	pruneFailed
)

type pruneResult struct{ cleared, unconfirmed, skipped, failed int }

func (r *pruneResult) add(o pruneOutcome, err error, s protocol.SessionSummary, p *core.Printer) {
	label := s.ShortID + " (" + protocol.FlattenDisplayText(s.Slug) + ")"
	switch o {
	case pruneCleared:
		r.cleared++
	case pruneUnconfirmed:
		r.unconfirmed++
	case pruneSkipped:
		r.skipped++
		p.Warn(fmt.Sprintf("prune: kept %s: it changed since the list was taken", label))
	case pruneFailed:
		r.failed++
		p.Warn(fmt.Sprintf("prune %s: %v", label, err))
	}
}

func (r pruneResult) summary() string {
	out := fmt.Sprintf("cleared %d", r.cleared)
	if r.unconfirmed > 0 {
		out += fmt.Sprintf(", sent %d unconfirmed (daemon predates acks; run 'rex daemon restart')", r.unconfirmed)
	}
	if r.skipped > 0 {
		out += fmt.Sprintf(", kept %d that changed", r.skipped)
	}
	return out
}

// prunePrecheckedDelete deletes s if a fresh snapshot still shows it
// prunable. The list was taken before the prompt, and a marked-done session
// goes back to work when someone types into it, so each target is checked
// again right before its delete.
func prunePrecheckedDelete(socket *core.SocketFlag, c *client.Client, s protocol.SessionSummary) (pruneOutcome, error) {
	now, found, err := freshSession(socket, s.ID)
	switch {
	case err != nil:
		return pruneFailed, err
	case !found:
		return pruneCleared, nil // already gone (the reaper got it first)
	case !tui.Prunable(now):
		return pruneSkipped, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	_, err = c.Do(ctx, protocol.IntentDelete, protocol.Delete{SessionID: s.ID})
	var de *client.Error
	switch {
	case err == nil:
		return pruneCleared, nil
	case errors.Is(err, client.ErrNoAcks):
		if err := c.Delete(s.ID); err != nil {
			return pruneFailed, err
		}
		return pruneUnconfirmed, nil
	case errors.As(err, &de) && de.Code == protocol.ErrCodeUnknownSession:
		return pruneCleared, nil // removed between the check and the delete
	case errors.Is(err, context.DeadlineExceeded):
		// The daemon may still be tearing the agent down.
		if _, found, ferr := freshSession(socket, s.ID); ferr == nil && !found {
			return pruneCleared, nil
		}
		return pruneFailed, err
	default:
		return pruneFailed, err
	}
}

// freshSession reads one session from a new snapshot on its own connection.
func freshSession(socket *core.SocketFlag, id string) (protocol.SessionSummary, bool, error) {
	c, err := socket.Dial()
	if err != nil {
		return protocol.SessionSummary{}, false, err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return protocol.SessionSummary{}, false, err
	}
	for _, s := range snap.Sessions {
		if s.ID == id {
			return s, true, nil
		}
	}
	return protocol.SessionSummary{}, false, nil
}
