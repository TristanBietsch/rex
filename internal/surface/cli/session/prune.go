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

// pruneIO is what RunPrune reads and writes; tests replace it.
var pruneIO = struct {
	in          io.Reader
	out, errOut io.Writer
	interactive func() bool
}{os.Stdin, os.Stdout, os.Stderr, stdinIsTerminal}

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

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
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 0 {
		return core.NewExitError(core.ExitInvalidArgs, "prune: takes no arguments")
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return err
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

	failed := 0
	for _, s := range targets {
		if err := deleteAcked(c, s.ID); err != nil {
			failed++
			fmt.Fprintf(errOut, "rex: prune %s (%s): %v\n", s.ShortID, protocol.FlattenDisplayText(s.Slug), err)
		}
	}
	fmt.Fprintf(out, "cleared %d%s\n", len(targets)-failed, note)
	if failed > 0 {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("prune: %d of %d failed", failed, len(targets)))
	}
	return nil
}

// deleteAcked deletes a session and waits for the daemon's Ack. A daemon
// that predates acks gets a plain Delete it cannot confirm.
func deleteAcked(c *client.Client, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	_, err := c.Do(ctx, protocol.IntentDelete, protocol.Delete{SessionID: id})
	if errors.Is(err, client.ErrNoAcks) {
		return c.Delete(id)
	}
	return err
}
