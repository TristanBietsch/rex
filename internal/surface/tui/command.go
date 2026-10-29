package tui

import (
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/audio"
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
	verb := parts[0]
	args := parts[1:]

	switch verb {
	case "q", "quit":
		m.Focus = FocusConfirmQuit
		return m, nil
	case "q!":
		m.Quitting = true
		return m, tea.Quit
	case "bg", "detach":
		_ = SaveTUIState(m)
		m.Quitting = true
		return m, tea.Quit
	case "help":
		m.Focus = FocusHelp
		if m.Audio != nil {
			m.Audio.Play(audio.EventOpen)
		}
		return m, nil
	case "settings":
		return openSettings(m)
	case "reload":
		return m, sendDaemonSIGHUP(m)
	case "filter":
		if len(args) == 1 {
			m.Filter = args[0]
		}
		return m, nil
	case "rm":
		if len(args) == 1 {
			id, msg := resolveLocal(m, args[0])
			if id == "" {
				m.Err = msg
				return m, nil
			}
			return m, deleteSessionCmd(m.Client, id)
		}
		m.Err = "rm: selector required"
		return m, nil
	case "rename":
		if len(args) == 2 {
			id, msg := resolveLocal(m, args[0])
			if id == "" {
				m.Err = msg
				return m, nil
			}
			if err := protocol.ValidateSlug(args[1]); err != nil {
				m.Err = "rename: " + err.Error()
				return m, nil
			}
			return m, renameCmd(m.Client, id, args[1])
		}
		m.Err = "rename: <selector> <new-slug>"
		return m, nil
	case "name":
		// :name           open the rename editor for the selection
		// :name <text…>   set it directly
		if len(args) == 0 {
			return openRename(m)
		}
		if m.SelectedID == "" || indexOfSelected(m) < 0 {
			m.Err = "name: no session selected"
			return m, nil
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, verb))
		return m, setNameCmd(namerOf(m), m.SelectedID, name)
	case "done", "undone":
		id, ok := commandTarget(&m, verb, args)
		if !ok {
			return m, nil
		}
		return toggleRetire(m, id)
	case "complete":
		id, ok := commandTarget(&m, verb, args)
		if !ok {
			return m, nil
		}
		return m, completeSessionCmd(m.Client, id)
	case "new":
		return openWizard(m)
	case "fail", "fails", "failed":
		return openFail(m)
	case "stats":
		m.Focus = FocusStats
		slog.Debug("tui: stats overlay opened")
		return m, nil
	default:
		m.Err = "unknown command: " + verb
	}
	return m, nil
}

// sendDaemonSIGHUP triggers a daemon-side tools.yaml reload by sending SIGHUP.
func sendDaemonSIGHUP(m Model) tea.Cmd {
	return func() tea.Msg {
		_ = daemonctl.Reload()
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
