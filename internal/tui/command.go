package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
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
			id := resolveLocal(m, args[0])
			if id == "" {
				m.Err = "no match for " + args[0]
				return m, nil
			}
			return m, deleteSessionCmd(m.Client, id)
		}
		m.Err = "rm: selector required"
		return m, nil
	case "rename":
		if len(args) == 2 {
			id := resolveLocal(m, args[0])
			if id == "" {
				m.Err = "no match for " + args[0]
				return m, nil
			}
			return m, renameCmd(m.Client, id, args[1])
		}
		m.Err = "rename: <selector> <new-slug>"
		return m, nil
	case "new":
		m.Focus = FocusWizard
		return m, nil
	default:
		m.Err = "unknown command: " + verb
	}
	return m, nil
}

// sendDaemonSIGHUP triggers a daemon-side tools.yaml reload by sending SIGHUP.
func sendDaemonSIGHUP(m Model) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("pgrep", "rex-daemon").Output()
		if err != nil {
			return nil
		}
		pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
		if pid == "" {
			return nil
		}
		_ = exec.Command("kill", "-HUP", pid).Run()
		return nil
	}
}

func resolveLocal(m Model, sel string) string {
	for _, s := range m.Sessions {
		if s.ID == sel || s.ShortID == sel || s.Slug == sel {
			return s.ID
		}
	}
	return ""
}

func renameCmd(c *client.Client, id, slug string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Rename(id, slug, ""); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}
