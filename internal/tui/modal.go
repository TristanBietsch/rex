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
	footer := styleDim.Render("esc to close")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)
	return border.Render(header + "\n" + body + "\n" + footer)
}
