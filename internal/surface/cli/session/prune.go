package session

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/tui"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// deleteTimeout bounds one Delete: the daemon kills the agent's process
// group and drains its output before it acknowledges.
const deleteTimeout = 15 * time.Second

// pruneIO is what RunPrune and RunRm read and write; tests replace it.
var pruneIO = struct {
	in          io.Reader
	out, errOut io.Writer
	interactive func() bool
}{os.Stdin, os.Stdout, os.Stderr, core.StdinIsTerminal}

// RunPrune deletes every session in the board's Completed column (done,
// failed, crashed, or marked done) except those whose agent is still
// working. It asks first; -yes skips the question and is required when
// stdin is not a terminal.
func RunPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	dryRun := fs.Bool("dry-run", false, "list the sessions that would be cleared; delete nothing")
	var yes bool
	fs.BoolVar(&yes, "yes", false, "do not ask (required when stdin is not a terminal)")
	fs.BoolVar(&yes, "y", false, "shorthand for -yes")
	rest, err := core.ParseArgs(fs, args)
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
	out, errOut := pruneIO.out, pruneIO.errOut
	if len(targets) == 0 {
		fmt.Fprintln(out, "nothing to clear"+note)
		return nil
	}
	if *dryRun {
		for _, s := range targets {
			fmt.Fprintf(out, "%s\t%s\t%s\n", s.ShortID, protocol.FlattenDisplayText(s.Slug), s.State)
		}
		fmt.Fprintf(out, "would clear %d%s\n", len(targets), note)
		return nil
	}
	if !yes {
		if !pruneIO.interactive() {
			return core.NewExitError(core.ExitOperationRefused,
				"prune: stdin is not a terminal; pass -yes to delete without asking, or -dry-run to list")
		}
		fmt.Fprintf(errOut, "clear %d completed session(s)? this kills their agents and removes their transcripts%s [y/N] ", len(targets), note)
		var ans string
		_, _ = fmt.Fscanln(pruneIO.in, &ans)
		if a := strings.ToLower(ans); a != "y" && a != "yes" {
			return core.NewExitError(core.ExitGeneric, "aborted")
		}
	}

	var r pruneResult
	for _, s := range targets {
		o, err := prunePrecheckedDelete(socket, c, s)
		r.add(o, err, s, errOut)
	}
	fmt.Fprintln(out, r.summary()+note)
	if r.failed > 0 {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("prune: %d of %d failed", r.failed, len(targets)))
	}
	return nil
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

func (r *pruneResult) add(o pruneOutcome, err error, s protocol.SessionSummary, errOut io.Writer) {
	label := s.ShortID + " (" + protocol.FlattenDisplayText(s.Slug) + ")"
	switch o {
	case pruneCleared:
		r.cleared++
	case pruneUnconfirmed:
		r.unconfirmed++
	case pruneSkipped:
		r.skipped++
		fmt.Fprintf(errOut, "rex: prune: kept %s: it changed since the list was taken\n", label)
	case pruneFailed:
		r.failed++
		fmt.Fprintf(errOut, "rex: prune %s: %v\n", label, err)
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
