package tui

import (
	"context"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/wire/client"
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

// reloadDaemonCmd asks the daemon on the TUI's socket to re-read
// tools.yaml with a Reload intent. It reaches the daemon only through that
// socket, never by signaling a process, and reports a failed request (no
// daemon answering, a write error) as a CmdErrMsg. A tools.yaml the daemon
// cannot load is not acknowledged (backlog FU-13); it shows in the daemon
// log.
func reloadDaemonCmd(m Model) tea.Cmd {
	reload := m.reloader
	if reload == nil {
		reload = daemonctl.ReloadDaemon
	}
	socket := m.Socket
	return func() tea.Msg {
		if err := reload(context.Background(), socket); err != nil {
			return CmdErrMsg{Op: "reload", Err: err}
		}
		return nil
	}
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
func resolveLocal(m Model, sel string) (id, errMsg string) {
	for _, s := range m.Sessions {
		if s.ID == sel {
			return s.ID, ""
		}
	}
	var slugHits []string
	for _, s := range m.Sessions {
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
	for _, s := range m.Sessions {
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
