package tui

import (
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/features/audio"
)

// attachHoldWindow is how long a lone escape rune (':' or 'q') typed in the
// attach popup waits for its twin before it is forwarded to the agent: "::"
// opens rex's command bar, "qq" detaches. Pastes arrive as one multi-rune key
// and are never intercepted.
const attachHoldWindow = 400 * time.Millisecond

// attachHoldTimeoutMsg flushes a held rune once the window passes.
type attachHoldTimeoutMsg struct {
	SessionID string
	Seq       int
}

// loneRune reports the single typed (not pasted) rune of k, if any.
func loneRune(k tea.KeyMsg) (rune, bool) {
	if k.Type != tea.KeyRunes || k.Paste || len(k.Runes) != 1 {
		return 0, false
	}
	return k.Runes[0], true
}

func isLoneColon(k tea.KeyMsg) bool {
	r, ok := loneRune(k)
	return ok && r == ':'
}

// interceptAttachHold implements the "::" and "qq" escapes. It returns
// handled=true when the key was consumed; prefix carries a held rune that
// must be sent ahead of the current key's bytes (or on its own when the key
// was consumed to start a new hold). allowQ is false in scroll mode, where a
// lone 'q' already means "leave scroll mode".
func interceptAttachHold(m Model, k tea.KeyMsg, allowQ bool) (out Model, cmd tea.Cmd, handled bool, prefix []byte) {
	a := m.Attach
	r, ok := loneRune(k)
	holdable := ok && (r == ':' || (allowQ && r == 'q'))
	if holdable && a.held == r {
		a.held = 0
		if r == 'q' {
			slog.Info("tui: attach qq detach", "session", a.SessionID)
			out, cmd = detach(m)
			return out, cmd, true, nil
		}
		m.Focus = FocusCommand
		m.CmdText = ""
		m.Err = ""
		slog.Info("tui: attach command bar opened", "session", a.SessionID)
		if m.Audio != nil {
			m.Audio.Play(audio.EventCommand)
		}
		return m, nil, true, nil
	}
	if a.held != 0 {
		prefix = []byte(string(a.held))
		a.held = 0
	}
	if holdable {
		a.held = r
		a.holdSeq++
		seq, id := a.holdSeq, a.SessionID
		return m, tea.Tick(attachHoldWindow, func(time.Time) tea.Msg {
			return attachHoldTimeoutMsg{SessionID: id, Seq: seq}
		}), true, prefix
	}
	return m, nil, false, prefix
}

// flushAttachHold forwards a held rune whose window expired untouched.
func flushAttachHold(m Model, msg attachHoldTimeoutMsg) (Model, tea.Cmd) {
	a := m.Attach
	if a == nil || a.held == 0 || a.SessionID != msg.SessionID || a.holdSeq != msg.Seq {
		return m, nil
	}
	held := a.held
	a.held = 0
	if a.Ended || a.Scrolling() {
		return m, nil // scroll mode never forwards keys
	}
	if err := a.Client.SendInput(a.SessionID, []byte(string(held))); err != nil {
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
	hint := styleMuted.Render("  q detach (or qq while attached) · done · kill · name <text> · ::: types \"::\" · esc back")
	line := styleArrowYellow.Render(":") + " " + stylePrimary.Render(m.CmdText) + cursorBlock(m)
	pad := width - lipgloss.Width(line) - lipgloss.Width(hint)
	if pad < 1 {
		return line
	}
	return line + strings.Repeat(" ", pad) + hint
}
