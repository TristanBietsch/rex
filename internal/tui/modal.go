package tui

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// ModalState lives on Model when Focus == FocusModal.
type ModalState struct {
	SessionID string
	Viewport  viewport.Model
	Buffer    strings.Builder
}

// openModal subscribes to the session and prepares a viewport.
func openModal(m Model, sessionID string) (Model, tea.Cmd) {
	if m.Audio != nil {
		m.Audio.Play("open")
	}
	w := m.Width
	h := m.Height
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	mw := w - 8
	if mw < 20 {
		mw = 20
	}
	mh := h - 6
	if mh < 10 {
		mh = 10
	}
	vp := viewport.New(mw, mh)
	m.Modal = &ModalState{SessionID: sessionID, Viewport: vp}
	m.Focus = FocusModal
	return m, subscribeSessionCmd(m.Client, sessionID)
}

func subscribeSessionCmd(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Subscribe(sessionID); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

// closeModal clears modal state and re-subscribes to board-wide only.
func closeModal(m Model) (Model, tea.Cmd) {
	if m.Audio != nil {
		m.Audio.Play("close")
	}
	m.Modal = nil
	m.Focus = FocusBoard
	if m.Client != nil {
		return m, func() tea.Msg {
			_ = m.Client.Subscribe("")
			return nil
		}
	}
	return m, nil
}

// handleModalOutput consumes SessionOutput events targeted at the open modal.
func handleModalOutput(m Model, env protocol.Envelope) Model {
	if m.Modal == nil || env.Type != protocol.EventSessionOutput {
		return m
	}
	var so protocol.SessionOutput
	if err := json.Unmarshal(env.Data, &so); err != nil {
		return m
	}
	if so.SessionID != m.Modal.SessionID {
		return m
	}
	m.Modal.Buffer.Write(so.Bytes)
	m.Modal.Viewport.SetContent(m.Modal.Buffer.String())
	m.Modal.Viewport.GotoBottom()
	return m
}

// keyToBytes converts a tea.KeyMsg into the byte sequence a PTY would expect.
// Returns nil for unsupported keys (caller drops them).
func keyToBytes(k tea.KeyMsg) []byte {
	switch k.Type {
	case tea.KeyRunes:
		return []byte(string(k.Runes))
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlY:
		return []byte{0x19}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	}
	return nil
}

// forwardKeyToModal converts a key event into a SendInput command.
func forwardKeyToModal(m Model, k tea.KeyMsg) tea.Cmd {
	if m.Modal == nil || m.Client == nil {
		return nil
	}
	b := keyToBytes(k)
	if b == nil {
		return nil
	}
	sessID := m.Modal.SessionID
	return func() tea.Msg {
		_ = m.Client.SendInput(sessID, b)
		return nil
	}
}

func renderModal(m Model) string {
	if m.Modal == nil {
		return ""
	}
	var sess protocol.SessionSummary
	for _, s := range m.Sessions {
		if s.ID == m.Modal.SessionID {
			sess = s
			break
		}
	}
	header := lipgloss.NewStyle().Padding(0, 1).Render(
		styleSlug.Render(sess.Slug) + "  " +
			styleDim.Render(sess.ToolID+" · "+sess.ModelID) + "  " +
			styleStateWorking.Render("["+string(sess.State)+"]"),
	)
	body := m.Modal.Viewport.View()
	footer := styleDim.Render("esc to detach · type to send input")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)
	return border.Render(header + "\n" + body + "\n" + footer)
}
