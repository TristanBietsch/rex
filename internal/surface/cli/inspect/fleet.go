package inspect

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
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
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	sub, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(sub) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "fleet: subcommand required (ls|set|unset|show)")
	}

	switch sub[0] {
	case "ls":
		if err := core.CheckArgs(sub, 1, 1, "rex fleet ls [-socket path]"); err != nil {
			return err
		}
		return runFleetLS(*socket, os.Stdout)
	case "set":
		if err := core.CheckArgs(sub, 3, 3, "rex fleet set <sel> <fleet> [-socket path]"); err != nil {
			return err
		}
		return runFleetSet(*socket, sub[1], sub[2])
	case "unset":
		if err := core.CheckArgs(sub, 2, 2, "rex fleet unset <sel> [-socket path]"); err != nil {
			return err
		}
		return runFleetSet(*socket, sub[1], "")
	case "show":
		if err := core.CheckArgs(sub, 2, 2, "rex fleet show <fleet> [-socket path]"); err != nil {
			return err
		}
		return runFleetShow(*socket, sub[1], os.Stdout)
	default:
		return core.NewExitError(core.ExitInvalidArgs, "fleet: unknown subcommand: "+sub[0])
	}
}

func runFleetLS(socket string, w io.Writer) error {
	c, err := client.Dial(socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}

	// Count sessions per fleet; "(none)" for unassigned.
	counts := map[string]int{}
	for _, s := range snap.Sessions {
		key := s.Fleet
		if key == "" {
			key = "(none)"
		}
		counts[key]++
	}

	// Sort for deterministic output.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	hdr := fmt.Sprintf("%-24s  %s", "FLEET", "SESSIONS")
	if _, err := fmt.Fprintln(w, hdr); err != nil {
		return err
	}
	for _, k := range keys {
		row := fmt.Sprintf("%-24s  %d", k, counts[k])
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
	}
	return nil
}

// runFleetSet assigns the session to fleet ("" clears it) and waits for the
// daemon to confirm, so a rejection (e.g. unknown session) reaches the user.
func runFleetSet(socket, selector, fleet string) error {
	c, err := client.Dial(socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, selector)
	if err != nil {
		return err
	}
	if err := c.SetSessionFleet(sess.ID, fleet); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	// SetSessionFleet carries no request id, so a rejection echoes "".
	if err := core.AwaitPatch(c, sess.ID, "", "fleet"); err != nil {
		return err
	}
	slog.Info("fleet: set", "session", sess.ID, "fleet", fleet)
	if fleet == "" {
		_, _ = fmt.Fprintf(os.Stdout, "cleared fleet on session %s (%s)\n", sess.ShortID, sess.Slug)
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "set fleet %q on session %s (%s)\n", fleet, sess.ShortID, sess.Slug)
	}
	return nil
}

func runFleetShow(socket, fleet string, w io.Writer) error {
	c, err := client.Dial(socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}

	result := snap.Sessions[:0:len(snap.Sessions)]
	for _, s := range snap.Sessions {
		if s.Fleet == fleet {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		_, err := fmt.Fprintf(w, "no sessions in fleet %q\n", fleet)
		return err
	}
	return core.WriteSessionsTable(w, result)
}
