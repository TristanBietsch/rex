package tui

import (
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/features/audio"
)

// attachColonWindow is how long a lone ':' typed in the attach popup waits
// for a second ':' (which opens rex's command bar) before it is forwarded to
// the agent. Pastes arrive as one multi-rune key and are never intercepted.
const attachColonWindow = 400 * time.Millisecond

// attachColonTimeoutMsg flushes a held ':' once the window passes.
type attachColonTimeoutMsg struct {
	SessionID string
	Seq       int
}

func isLoneColon(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && !k.Paste && len(k.Runes) == 1 && k.Runes[0] == ':'
}

// interceptAttachColon implements the "::" escape. It returns handled=true
// when the key was consumed; prefix carries a held ':' that must be sent
// ahead of the current key's bytes.
func interceptAttachColon(m Model, k tea.KeyMsg) (out Model, cmd tea.Cmd, handled bool, prefix []byte) {
	a := m.Attach
	if isLoneColon(k) {
		if a.colonPending {
			a.colonPending = false
			m.Focus = FocusCommand
			m.CmdText = ""
			m.Err = ""
			slog.Info("tui: attach command bar opened", "session", a.SessionID)
			if m.Audio != nil {
				m.Audio.Play(audio.EventCommand)
			}
			return m, nil, true, nil
		}
		a.colonPending = true
		a.colonSeq++
		seq, id := a.colonSeq, a.SessionID
		return m, tea.Tick(attachColonWindow, func(time.Time) tea.Msg {
			return attachColonTimeoutMsg{SessionID: id, Seq: seq}
		}), true, nil
	}
	if a.colonPending {
		a.colonPending = false
		return m, nil, false, []byte{':'}
	}
	return m, nil, false, nil
}

// flushAttachColon forwards a held ':' whose window expired untouched.
func flushAttachColon(m Model, msg attachColonTimeoutMsg) (Model, tea.Cmd) {
	a := m.Attach
	if a == nil || !a.colonPending || a.SessionID != msg.SessionID || a.colonSeq != msg.Seq {
		return m, nil
	}
	a.colonPending = false
	if a.Ended || a.Scrolling() {
		return m, nil // scroll mode never forwards keys
	}
	if err := a.Client.SendInput(a.SessionID, []byte{':'}); err != nil {
		m.Err = "attach: " + err.Error()
	}
	return m, nil
}

// updateAttachCommandKey edits the command bar opened with "::" over the
// attach popup.
func updateAttachCommandKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		m.Focus = FocusAttach
		m.CmdText = ""
		return m, nil
	case tea.KeyCtrlCloseBracket:
		m.CmdText = ""
		return detach(m)
	case tea.KeyEnter:
		line := m.CmdText
		m.CmdText = ""
		return executeAttachCommand(m, line)
	case tea.KeyBackspace:
		if m.CmdText == "" {
			m.Focus = FocusAttach
			return m, nil
		}
		m.CmdText = dropLastRune(m.CmdText)
		return m, nil
	case tea.KeySpace:
		m.CmdText += " "
		return m, nil
	case tea.KeyRunes:
		// ":::" types a literal "::" into the agent (e.g. C++ std::).
		if m.CmdText == "" && isLoneColon(k) {
			m.Focus = FocusAttach
			if err := m.Attach.Client.SendInput(m.Attach.SessionID, []byte("::")); err != nil {
				m.Err = "attach: " + err.Error()
			}
			return m, nil
		}
		m.CmdText += string(k.Runes)
		return m, nil
	}
	return m, nil
}

// executeAttachCommand runs a command typed in the attach command bar. A few
// verbs act on the attached session; anything else closes the popup and
// runs as a normal board command.
func executeAttachCommand(m Model, line string) (Model, tea.Cmd) {
	a := m.Attach
	line = strings.TrimSpace(line)
	m.Focus = FocusAttach
	if line == "" {
		return m, nil
	}
	parts := strings.Fields(line)
	verb, rest := parts[0], strings.TrimSpace(strings.TrimPrefix(line, parts[0]))
	slog.Info("tui: attach command", "session", a.SessionID, "verb", verb)
	switch verb {
	case "q", "q!", "quit", "detach", "close":
		return detach(m)
	case "done", "undone":
		return toggleRetire(m, a.SessionID)
	case "complete", "kill":
		if m.Client == nil {
			return m, nil
		}
		return m, completeSessionCmd(m.Client, a.SessionID)
	case "name", "rename":
		return m, setNameCmd(namerOf(m), a.SessionID, rest)
	}
	m, off := detach(m)
	m, cmd := executeCommand(m, line)
	return m, tea.Batch(off, cmd)
}

// renderAttachCommandBar replaces the attach footer while "::" is open.
func renderAttachCommandBar(m Model, width int) string {
	hint := styleMuted.Render("  q detach · done · kill · name <text> · ::: types \"::\" · esc back")
	line := styleArrowYellow.Render(":") + " " + stylePrimary.Render(m.CmdText) + cursorBlock(m)
	pad := width - lipgloss.Width(line) - lipgloss.Width(hint)
	if pad < 1 {
		return line
	}
	return line + strings.Repeat(" ", pad) + hint
}
