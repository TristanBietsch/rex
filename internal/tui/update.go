package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// Update is the central Bubble Tea handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case DaemonEventMsg:
		m = m.applyEvent(msg.Env)
		if m.Modal != nil {
			m = handleModalOutput(m, msg.Env)
		}
		return m, listenDaemon(m.Client)
	case DaemonErrMsg:
		m.Err = msg.Err.Error()
		return m, tea.Quit
	case SpinnerTickMsg:
		m.SpinnerTick++
		return m, tickSpinner()
	case tea.KeyMsg:
		return updateKey(m, msg)
	}
	return m, nil
}

func updateKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	// Focused prompt: characters are input.
	if m.Focus == FocusPrompt {
		return updatePromptKey(m, k)
	}

	// Board focus: navigation, mode switches, single-key actions, and 'd' chord.
	if m.Focus == FocusBoard {
		// Chord handling — only the first 'd' arms the chord; second 'd' fires.
		if k.String() == "d" {
			if m.PendingChord == "d" {
				m.PendingChord = ""
				if m.SelectedID != "" {
					return m, deleteSessionCmd(m.Client, m.SelectedID)
				}
				return m, nil
			}
			m.PendingChord = "d"
			return m, nil
		}
		// Any other key clears the pending chord.
		m.PendingChord = ""

		switch k.String() {
		case "ctrl+c", "q":
			m.Quitting = true
			return m, tea.Quit
		case "j", "down":
			return moveSelection(m, 1), nil
		case "k", "up":
			return moveSelection(m, -1), nil
		case "g":
			rows := orderedSessions(m)
			if len(rows) > 0 {
				m.SelectedID = rows[0].ID
			}
			return m, nil
		case "G":
			rows := orderedSessions(m)
			if len(rows) > 0 {
				m.SelectedID = rows[len(rows)-1].ID
			}
			return m, nil
		case "1":
			return jumpToSection(m, protocol.StateNeedsInput), nil
		case "2":
			return jumpToSection(m, protocol.StateWorking), nil
		case "3":
			return jumpToSection(m, protocol.StateDone), nil
		case "t":
			return cycleFilter(m), nil
		case "n":
			return openWizard(m)
		case "?":
			m.Focus = FocusHelp
			return m, nil
		case "i":
			m.Focus = FocusPrompt
			m.Err = ""
			return m, nil
		case "enter":
			if m.SelectedID == "" {
				return m, nil
			}
			return openModal(m, m.SelectedID)
		case ":":
			m.Focus = FocusCommand
			m.CmdText = ""
			m.Err = ""
			return m, nil
		}
	}

	if m.Focus == FocusCommand {
		return updateCommandKey(m, k)
	}
	if m.Focus == FocusConfirmQuit {
		return updateConfirmQuitKey(m, k)
	}
	if m.Focus == FocusModal {
		if k.Type == tea.KeyEsc {
			return closeModal(m)
		}
		return m, nil
	}
	if m.Focus == FocusWizard {
		return updateWizardKey(m, k)
	}
	if m.Focus == FocusHelp {
		switch k.String() {
		case "esc", "?":
			m.Focus = FocusBoard
		}
		return m, nil
	}

	return m, nil
}

func updatePromptKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.Focus = FocusBoard
		m.PromptText = ""
		return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(m.PromptText)
		m.PromptText = ""
		m.Focus = FocusBoard
		if text == "" {
			return m, nil
		}
		return m, spawnSessionCmd(m.Client, text)
	case tea.KeyBackspace:
		if len(m.PromptText) > 0 {
			m.PromptText = m.PromptText[:len(m.PromptText)-1]
		}
		return m, nil
	case tea.KeyRunes:
		m.PromptText += string(k.Runes)
		return m, nil
	case tea.KeySpace:
		m.PromptText += " "
		return m, nil
	}
	return m, nil
}

func spawnSessionCmd(c *client.Client, prompt string) tea.Cmd {
	return func() tea.Msg {
		slug := deriveSlugFromPrompt(prompt)
		if slug == "" {
			slug = "session"
		}
		cwd, _ := os.Getwd()
		if err := c.NewSession(protocol.NewSession{
			ToolID:        "echo",
			ModelID:       "short",
			Slug:          slug,
			CWD:           cwd,
			InitialPrompt: prompt,
		}); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func deleteSessionCmd(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Delete(sessionID); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func updateCommandKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.Focus = FocusBoard
		m.CmdText = ""
		return m, nil
	case tea.KeyEnter:
		cmd := m.CmdText
		m.CmdText = ""
		m.Focus = FocusBoard
		return executeCommand(m, cmd)
	case tea.KeyBackspace:
		if len(m.CmdText) > 0 {
			m.CmdText = m.CmdText[:len(m.CmdText)-1]
		}
		return m, nil
	case tea.KeyRunes:
		m.CmdText += string(k.Runes)
		return m, nil
	case tea.KeySpace:
		m.CmdText += " "
		return m, nil
	}
	return m, nil
}

func updateConfirmQuitKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y", "enter":
		m.Quitting = true
		return m, tea.Quit
	case "n", "N", "esc":
		m.Focus = FocusBoard
		return m, nil
	}
	return m, nil
}

func deriveSlugFromPrompt(p string) string {
	s := strings.ToLower(p)
	if len(s) > 32 {
		s = s[:32]
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
