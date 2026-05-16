package cli

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/tristanbietsch/rex/internal/client"
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
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	sub := fs.Args()
	if len(sub) == 0 {
		return NewExitError(ExitInvalidArgs, "fleet: subcommand required (ls|set|unset|show)")
	}

	switch sub[0] {
	case "ls":
		return runFleetLS(*socket, os.Stdout)
	case "set":
		if len(sub) < 3 {
			return NewExitError(ExitInvalidArgs, "fleet set: usage: fleet set <selector> <fleet>")
		}
		return runFleetSet(*socket, sub[1], sub[2])
	case "unset":
		if len(sub) < 2 {
			return NewExitError(ExitInvalidArgs, "fleet unset: usage: fleet unset <selector>")
		}
		return runFleetUnset(*socket, sub[1])
	case "show":
		if len(sub) < 2 {
			return NewExitError(ExitInvalidArgs, "fleet show: usage: fleet show <fleet>")
		}
		return runFleetShow(*socket, sub[1], os.Stdout)
	default:
		return NewExitError(ExitInvalidArgs, "fleet: unknown subcommand: "+sub[0])
	}
}

func runFleetLS(socket string, w io.Writer) error {
	c, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
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

func runFleetSet(socket, selector, fleet string) error {
	c, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, selector)
	if err != nil {
		return err
	}

	// Reconnect — ResolveSelector consumed Hello.
	c2, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c2.Close()
	if _, err := c2.Hello("rex-cli"); err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	if err := c2.SetSessionFleet(sess.ID, fleet); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	slog.Info("fleet: set", "session", sess.ID, "fleet", fleet)
	_, _ = fmt.Fprintf(os.Stdout, "set fleet %q on session %s (%s)\n", fleet, sess.ShortID, sess.Slug)
	return nil
}

func runFleetUnset(socket, selector string) error {
	c, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, selector)
	if err != nil {
		return err
	}

	c2, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c2.Close()
	if _, err := c2.Hello("rex-cli"); err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	if err := c2.SetSessionFleet(sess.ID, ""); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	slog.Info("fleet: unset", "session", sess.ID, "fleet", "")
	_, _ = fmt.Fprintf(os.Stdout, "cleared fleet on session %s (%s)\n", sess.ShortID, sess.Slug)
	return nil
}

func runFleetShow(socket, fleet string, w io.Writer) error {
	c, err := client.Dial(socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	snap, err := c.Hello("rex-cli")
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
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
	return WriteSessionsTable(w, result)
}
