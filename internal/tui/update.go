package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// attachDoneMsg signals that a child `rex attach` process exited and we should
// resume the TUI. We surface any error in the status line.
type attachDoneMsg struct {
	err error
}

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
		return m, nil
	case DaemonEventMsg:
		m = m.applyEvent(msg.Env)
		return m, listenDaemon(m.Client)
	case DaemonErrMsg:
		// Surface the error in the status line, don't take down the TUI.
		m.Err = msg.Err.Error()
		return m, listenDaemon(m.Client)
	case SpinnerTickMsg:
		m.SpinnerTick++
		return m, tickSpinner()
	case attachDoneMsg:
		if msg.err != nil {
			m.Err = "attach: " + msg.err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		return updateKey(m, msg)
	case tea.MouseMsg:
		return updateMouse(m, msg)
	}
	return m, nil
}

func updateMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	// Click-to-select + double-click-to-open. Exact row math requires layout
	// coordinates; we use a header-offset heuristic.
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
		return attachSession(m, rows[idx].ID)
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
		// Chord handling — only the first 'd' arms the chord; second 'd' opens
		// the delete confirmation prompt.
		if k.String() == "d" {
			if m.PendingChord == "d" {
				m.PendingChord = ""
				if m.SelectedID != "" {
					m.PendingDeleteID = m.SelectedID
					m.Focus = FocusConfirmDelete
					if m.Audio != nil {
						m.Audio.Play(audio.EventOpen)
					}
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
			if m.Audio != nil {
				m.Audio.Play(audio.EventFilter)
			}
			return cycleFilter(m), nil
		case "n":
			return openWizard(m)
		case "?":
			m.Focus = FocusHelp
			if m.Audio != nil {
				m.Audio.Play(audio.EventOpen)
			}
			return m, nil
		case "S":
			return openSettings(m)
		case "i":
			m.Focus = FocusPrompt
			m.Err = ""
			return m, nil
		case "enter":
			// Only attach when the selection points to a session that's
			// actually on the board. A SelectedID restored from disk can
			// reference a failed/crashed session that's no longer visible.
			if indexOfSelected(m) < 0 {
				return m, nil
			}
			return attachSession(m, m.SelectedID)
		case ":":
			m.Focus = FocusCommand
			m.CmdText = ""
			m.Err = ""
			if m.Audio != nil {
				m.Audio.Play(audio.EventCommand)
			}
			return m, nil
		}
	}

	if m.Focus == FocusCommand {
		return updateCommandKey(m, k)
	}
	if m.Focus == FocusConfirmQuit {
		return updateConfirmQuitKey(m, k)
	}
	if m.Focus == FocusConfirmDelete {
		return updateConfirmDeleteKey(m, k)
	}
	if m.Focus == FocusWizard {
		return updateWizardKey(m, k)
	}
	if m.Focus == FocusHelp {
		switch k.String() {
		case "esc", "?":
			m.Focus = FocusBoard
			if m.Audio != nil {
				m.Audio.Play(audio.EventClose)
			}
		}
		return m, nil
	}
	if m.Focus == FocusSettings {
		return updateSettingsKey(m, k)
	}

	return m, nil
}

// attachSession suspends the TUI and runs `rex attach <id>` as a child process so
// the agent's terminal output renders natively in the user's terminal. When the
// child exits (Ctrl+]) we resume the TUI via attachDoneMsg.
func attachSession(m Model, sessionID string) (Model, tea.Cmd) {
	self, err := os.Executable()
	if err != nil {
		m.Err = fmt.Sprintf("attach: locate self: %v", err)
		return m, nil
	}
	if m.Audio != nil {
		m.Audio.Play("open")
	}
	args := []string{"attach"}
	if m.Socket != "" {
		args = append(args, "--socket", m.Socket)
	}
	args = append(args, sessionID)
	child := exec.Command(self, args...)
	return m, tea.ExecProcess(child, func(err error) tea.Msg {
		return attachDoneMsg{err: err}
	})
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

func updateConfirmDeleteKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		id := m.PendingDeleteID
		m.PendingDeleteID = ""
		m.Focus = FocusBoard
		if id == "" {
			return m, nil
		}
		if m.Audio != nil {
			m.Audio.Play(audio.EventDelete)
		}
		return m, deleteSessionCmd(m.Client, id)
	case "n", "N", "esc", "enter":
		m.PendingDeleteID = ""
		m.Focus = FocusBoard
		if m.Audio != nil {
			m.Audio.Play(audio.EventClose)
		}
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
