package tui

import (
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

var (
	lastMouseRow  = -1
	lastMouseTime time.Time
)

// Update is the central Bubble Tea handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Modal != nil {
			m = resizeModal(m, m.Width, m.Height)
		}
		return m, nil
	case DaemonEventMsg:
		m = m.applyEvent(msg.Env)
		if m.Modal != nil {
			m = handleModalOutput(m, msg.Env)
		}
		return m, listenDaemon(m.Client)
	case DaemonErrMsg:
		// Surface the error in the status line, don't take down the TUI.
		m.Err = msg.Err.Error()
		return m, listenDaemon(m.Client)
	case SpinnerTickMsg:
		m.SpinnerTick++
		return m, tickSpinner()
	case tea.KeyMsg:
		return updateKey(m, msg)
	case tea.MouseMsg:
		return updateMouse(m, msg)
	}
	return m, nil
}

func updateMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	// Plan C ships click-to-select + double-click-to-open. Exact row math
	// requires layout coordinates; we use a header-offset heuristic.
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	rows := orderedSessions(m)
	if len(rows) == 0 {
		return m, nil
	}
	// Board starts at approximately Y=3 (header) + section title rows.
	idx := msg.Y - 4
	if idx < 0 || idx >= len(rows) {
		lastMouseRow = -1
		return m, nil
	}
	m.SelectedID = rows[idx].ID
	now := time.Now()
	if lastMouseRow == idx && now.Sub(lastMouseTime) < 350*time.Millisecond {
		// Double-click — open modal.
		return openModal(m, rows[idx].ID)
	}
	lastMouseRow = idx
	lastMouseTime = now
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
			m.ScrollOffset = 0
			return ensureVisible(m), nil
		case "G":
			rows := orderedSessions(m)
			if len(rows) > 0 {
				m.SelectedID = rows[len(rows)-1].ID
			}
			return ensureVisible(m), nil
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
		case "S":
			return openSettings(m)
		case "/":
			return openSlash(m)
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
		return updateModalKey(m, k)
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
	if m.Focus == FocusSettings {
		return updateSettingsKey(m, k)
	}
	if m.Focus == FocusSlash {
		return updateSlashKey(m, k)
	}

	return m, nil
}

// updateModalKey handles keystrokes while a session modal is open.
// Ctrl+] always detaches. ":" enters a vim-style command line (':q' to quit).
// Everything else is forwarded to the underlying session PTY.
func updateModalKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Modal == nil {
		return m, nil
	}
	if k.Type == tea.KeyCtrlCloseBracket {
		return closeModal(m)
	}
	if m.Modal.CmdMode {
		switch k.Type {
		case tea.KeyEsc:
			m.Modal.CmdMode = false
			m.Modal.CmdText = ""
			return m, nil
		case tea.KeyEnter:
			cmd := strings.TrimSpace(m.Modal.CmdText)
			m.Modal.CmdMode = false
			m.Modal.CmdText = ""
			switch cmd {
			case "q", "quit", "bg", "detach":
				return closeModal(m)
			}
			return m, nil
		case tea.KeyBackspace:
			if len(m.Modal.CmdText) > 0 {
				m.Modal.CmdText = m.Modal.CmdText[:len(m.Modal.CmdText)-1]
			}
			return m, nil
		case tea.KeyRunes:
			m.Modal.CmdText += string(k.Runes)
			return m, nil
		case tea.KeySpace:
			m.Modal.CmdText += " "
			return m, nil
		}
		return m, nil
	}
	// Trigger modal command mode on bare colon.
	if k.Type == tea.KeyRunes && string(k.Runes) == ":" {
		m.Modal.CmdMode = true
		m.Modal.CmdText = ""
		return m, nil
	}
	if cmd := forwardKeyToModal(m, k); cmd != nil {
		return m, cmd
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
