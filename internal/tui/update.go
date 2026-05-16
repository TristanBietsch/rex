package tui

import (
	"fmt"
	"log/slog"
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
		resizeAttach(m)
		return m, nil
	case AttachOutputMsg:
		if m.Attach != nil && msg.SessionID == m.Attach.SessionID && len(msg.Bytes) > 0 {
			_, _ = m.Attach.Term.Write(msg.Bytes)
		}
		if m.Attach != nil {
			return m, listenAttach(m.Attach)
		}
		return m, nil
	case AttachClosedMsg:
		if m.Attach != nil && msg.SessionID == m.Attach.SessionID {
			m.Attach.Ended = true
			if msg.Err != nil {
				m.Attach.EndedMsg = msg.Err.Error()
			} else {
				m.Attach.EndedMsg = "session ended"
			}
		}
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
	case settingsResultMsg:
		m.Store = msg.Store
		m.StorePath = msg.Path
		if !msg.Found {
			return m.appendBootStep(bootStepMsg{
				Name: "settings.load", Status: stepSkip, Desc: "no config file (defaults)", Dur: msg.Dur,
			})
		}
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "settings.load", Status: stepWarn, Desc: fmt.Sprintf("%v (defaults)", msg.Err), Err: msg.Err, Dur: msg.Dur,
			})
		}
		return m.appendBootStep(bootStepMsg{
			Name: "settings.load", Status: stepOK, Desc: msg.Path, Dur: msg.Dur,
		})

	case dialResultMsg:
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "client.dial", Status: stepFail, Err: msg.Err, Desc: msg.Err.Error(), Dur: msg.Dur,
			})
		}
		m.Client = msg.C
		return m.appendBootStep(bootStepMsg{
			Name: "client.dial", Status: stepOK,
			Desc: fmt.Sprintf("connected · %s", msg.Dur.Truncate(time.Millisecond)),
			Dur:  msg.Dur,
		})

	case snapshotResultMsg:
		if msg.Err != nil {
			return m.appendBootStep(bootStepMsg{
				Name: "handshake", Status: stepFail, Err: msg.Err, Desc: msg.Err.Error(), Dur: msg.Dur,
			})
		}
		m.Sessions = msg.Snap.Sessions
		if msg.Snap.Filter != "" {
			m.Filter = msg.Snap.Filter
		}
		return m.appendBootStep(bootStepMsg{
			Name: "handshake", Status: stepOK,
			Desc: "接続 · rex-tui",
			Dur:  msg.Dur,
		})

	case bootStepMsg:
		return m.appendBootStep(msg)
	case bootMinElapsedMsg:
		m.BootMinDone = true
		if m.BootDone && !m.BootFailed {
			return m.handOffToBoard()
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
	if m.Store != nil {
		if enabled, ok := m.Store.Get("mouse_enabled").(bool); ok && !enabled {
			return m, nil
		}
	}
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
		return openAttach(m, rows[idx].ID)
	}
	lastMouseRow = idx
	lastMouseTime = now
	return m, nil
}

func updateKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	// Boot splash grabs keys to allow quit during long/failed startup.
	if m.Focus == FocusBoot {
		switch k.String() {
		case "ctrl+c", "q", "esc":
			m.Quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	// Attach popup grabs every key (forwards them to the agent's PTY).
	if m.Focus == FocusAttach {
		return updateAttachKey(m, k)
	}
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
		case "ctrl+c":
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
			return chooseAttach(m, m.SelectedID)
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
	if m.Focus == FocusFail {
		return updateFailKey(m, k)
	}

	return m, nil
}

// nativeAttachTools is the per-tool override that bypasses the in-process
// popup and hands the user the full terminal via tea.ExecProcess. Empty by
// default — every agent goes through the popup (which is what works reliably
// for ollama and, with the recent fixes, the others). Re-add a tool here only
// if its TUI provably can't be projected through vt10x.
var nativeAttachTools = map[string]bool{}

// chooseAttach picks the popup or native exec attach based on the session's
// tool. See nativeAttachTools for the rationale.
func chooseAttach(m Model, sessionID string) (Model, tea.Cmd) {
	toolID := ""
	for _, s := range m.Sessions {
		if s.ID == sessionID {
			toolID = s.ToolID
			break
		}
	}
	if nativeAttachTools[toolID] {
		slog.Info("tui: attach native", "session", sessionID, "tool", toolID)
		return attachSession(m, sessionID)
	}
	slog.Info("tui: attach popup", "session", sessionID, "tool", toolID)
	return openAttach(m, sessionID)
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
	if k.Type == tea.KeyEsc || k.String() == "esc" {
		m.Focus = FocusBoard
		m.PromptText = ""
		return m, nil
	}
	switch k.Type {
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
	if k.Type == tea.KeyEsc || k.String() == "esc" {
		m.Focus = FocusBoard
		m.CmdText = ""
		return m, nil
	}
	switch k.Type {
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

// appendBootStep runs the shared "append row, chime, advance, maybe handoff"
// logic for both bootStepMsg and the special msgs (dial/snapshot) that produce
// a row plus carry side data into Model.
func (m Model) appendBootStep(msg bootStepMsg) (tea.Model, tea.Cmd) {
	m.BootLog = append(m.BootLog, bootLine{
		Name: msg.Name, Status: msg.Status, Desc: msg.Desc, Err: msg.Err,
	})
	if msg.Status == stepFail {
		m.BootFailed = true
		m.BootError = msg.Err
		if m.Audio != nil {
			m.Audio.Play(audio.EventBootFail)
		}
		return m, nil
	}
	if ev := chimeFor(msg.Status); ev != "" && m.Audio != nil {
		m.Audio.Play(ev)
	}
	m.BootStep++
	if m.BootStep >= len(bootSequence) {
		m.BootDone = true
		if m.BootMinDone {
			return m.handOffToBoard()
		}
		return m, nil
	}
	return m, delayThen(nextStep(m))
}

// update is the test-friendly entry point that runs the bootMinElapsedMsg
// portion of Update's switch in isolation. Tests use this; production uses
// the real Update.
func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(bootMinElapsedMsg); ok {
		m.BootMinDone = true
		if m.BootDone && !m.BootFailed {
			return m.handOffToBoard()
		}
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
