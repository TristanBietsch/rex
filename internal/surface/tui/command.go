package tui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// executeCommand parses and dispatches a `:` command line.
func executeCommand(m Model, line string) (Model, tea.Cmd) {
	line = strings.TrimSpace(line)
	if line == "" {
		return m, nil
	}
	parts := strings.Fields(line)
	call := cmdCall{Verb: parts[0], Args: parts[1:], Line: line}
	c, ok := lookupCommand(call.Verb)
	if !ok {
		m.Err = "unknown command: " + call.Verb + " (try :help)"
		return m, nil
	}
	if c.Flag != "" && refuseOff(&m, c.Flag) {
		return m, nil
	}
	return c.Run(m, call)
}

// reloadDaemonCmd asks the daemon on the TUI's socket to reload every
// layer with a Reload intent. It reaches the daemon only through that
// socket, never by signaling a process. A failed request (no daemon
// answering, a write error) is a CmdErrMsg; the daemon's report comes back
// as a daemonReloadedMsg.
func reloadDaemonCmd(m Model) tea.Cmd {
	reload := m.reportReloader
	if reload == nil {
		reload = daemonctl.ReloadDaemonReport
	}
	socket := m.Socket
	return func() tea.Msg {
		rep, err := reload(context.Background(), socket)
		if err != nil {
			return CmdErrMsg{Op: "reload", Err: err}
		}
		return daemonReloadedMsg{Report: rep}
	}
}

// daemonReloadedMsg carries the daemon's reload report (R20).
type daemonReloadedMsg struct{ Report protocol.ReloadReport }

// applyDaemonReload puts a reload report on the status line: the first
// failed layer as an error, else what changed and what needs a restart.
func applyDaemonReload(m Model, msg daemonReloadedMsg) Model {
	rep := msg.Report
	if n := len(rep.Errors); n > 0 {
		text := "reload: " + rep.Errors[0]
		if n > 1 {
			text += fmt.Sprintf(" (+%d more, see rex reload)", n-1)
		}
		m.Err = protocol.FlattenDisplayText(text)
		return m
	}
	if rep.Legacy {
		m.Notice = "reloaded"
		return m
	}
	parts := []string{"reloaded"}
	if len(rep.Changed) == 0 {
		parts = append(parts, "nothing changed")
	} else {
		parts = append(parts, "changed: "+strings.Join(rep.Changed, ", "))
	}
	if len(rep.Restart) > 0 {
		parts = append(parts, "needs daemon restart: "+strings.Join(rep.Restart, ", "))
	}
	m.Notice = protocol.FlattenDisplayText(strings.Join(parts, " · "))
	return m
}

// commandTarget resolves an optional selector argument, defaulting to the
// selected row. Sets m.Err and returns ok=false when nothing matches.
func commandTarget(m *Model, verb string, args []string) (string, bool) {
	if len(args) > 0 {
		id, msg := resolveLocal(*m, args[0])
		if id == "" {
			m.Err = msg
			return "", false
		}
		return id, true
	}
	if m.SelectedID == "" || indexOfSelected(*m) < 0 {
		m.Err = verb + ": no session selected"
		return "", false
	}
	return m.SelectedID, true
}

// resolveLocal resolves a selector against the board: full id, then slug,
// then short id (the CLI's order). It returns the session id, or "" and a
// status-line message. Slugs shared by several sessions (restored from an
// older daemon) are reported as ambiguous rather than resolved to the first.
// On a workspace board only that workspace's sessions match.
func resolveLocal(m Model, sel string) (id, errMsg string) {
	sessions := scopedSessions(m) // a workspace board names only its own
	for _, s := range sessions {
		if s.ID == sel {
			return s.ID, ""
		}
	}
	var slugHits []string
	for _, s := range sessions {
		if s.Slug == sel {
			slugHits = append(slugHits, s.ShortID)
			id = s.ID
		}
	}
	switch {
	case len(slugHits) == 1:
		return id, ""
	case len(slugHits) > 1:
		slog.Warn("tui: ambiguous selector", "selector", sel, "matches", slugHits)
		return "", "ambiguous: " + sel + " matches " + strings.Join(slugHits, ", ") + "; use short id"
	}
	for _, s := range sessions {
		if s.ShortID == sel {
			return s.ID, ""
		}
	}
	return "", "no match for " + sel
}

// renameCmd sends the slug change. A rejection (slug in use, bad format)
// comes back as an Error event echoing reqID and lands on the status line.
func renameCmd(c *client.Client, id, slug string) tea.Cmd {
	return func() tea.Msg {
		reqID := "tui-rename-" + id
		slog.Info("tui: rename intent sent", "session", id, "slug", slug, "req_id", reqID)
		if err := c.Rename(id, slug, "", reqID); err != nil {
			slog.Error("tui: rename intent failed", "session", id, "err", err)
			return CmdErrMsg{Op: "rename", Err: err}
		}
		return nil
	}
}
