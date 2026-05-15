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

// modalViewportSize returns the viewport (width, height) for a w×h terminal.
// The modal occupies the whole screen with 2-column inset, top bar + 2 HRs + footer.
func modalViewportSize(w, h int) (int, int) {
	vw := w - 4
	if vw < 20 {
		vw = 20
	}
	vh := h - 6 // top(1) + hr(1) + footer(1) + hr(1) + 2 padding lines
	if vh < 4 {
		vh = 4
	}
	return vw, vh
}

// openModal subscribes to the session and prepares a viewport.
func openModal(m Model, sessionID string) (Model, tea.Cmd) {
	if m.Audio != nil {
		m.Audio.Play("open")
	}
	w, h := m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	vw, vh := modalViewportSize(w, h)
	vp := viewport.New(vw, vh)
	m.Modal = &ModalState{SessionID: sessionID, Viewport: vp}
	m.Focus = FocusModal
	return m, subscribeSessionCmd(m.Client, sessionID)
}

// resizeModal resizes the open viewport to the new terminal size.
func resizeModal(m Model, w, h int) Model {
	if m.Modal == nil {
		return m
	}
	vw, vh := modalViewportSize(w, h)
	m.Modal.Viewport.Width = vw
	m.Modal.Viewport.Height = vh
	m.Modal.Viewport.SetContent(m.Modal.Buffer.String())
	m.Modal.Viewport.GotoBottom()
	return m
}

func subscribeSessionCmd(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := c.Subscribe(sessionID); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

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
	case tea.KeyEsc:
		return []byte{0x1b}
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

// renderModalFull renders the modal as a full-screen view (the board is replaced).
// w×h is the full terminal size.
func renderModalFull(m Model, w, h int) string {
	if m.Modal == nil {
		return strings.Repeat(padLine("", w)+"\n", h)
	}
	var sess protocol.SessionSummary
	for _, s := range m.Sessions {
		if s.ID == m.Modal.SessionID {
			sess = s
			break
		}
	}

	title := styleSlug.Render(sess.Slug)
	meta := styleDim.Render("  " + sess.ShortID + " · " + sess.ToolID + " · " + sess.ModelID)
	if sess.Effort != "" {
		meta = styleDim.Render("  " + sess.ShortID + " · " + sess.ToolID + " · " + sess.ModelID + " · " + sess.Effort)
	}
	pill := stateBadge(sess.State, m.SpinnerTick)
	left := "  " + title + meta
	right := pill + "  "
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	topBar := padLine(left+repeatRune(' ', gap)+right, w)
	hr := renderHR(w)

	footerHint := styleDim.Render("ctrl+] to detach · type to send input")
	footer := padLine("  "+footerHint, w)

	body := m.Modal.Viewport.View()
	bodyLines := strings.Split(body, "\n")
	// Place body inside an indented area for visual breathing room.
	indented := make([]string, len(bodyLines))
	for i, ln := range bodyLines {
		indented[i] = padLine("  "+ln, w)
	}
	body = strings.Join(indented, "\n")

	out := strings.Join([]string{
		padLine("", w),
		topBar,
		hr,
		padLine("", w),
		body,
		padLine("", w),
		hr,
		footer,
	}, "\n")
	return fitHeight(out, w, h)
}

func stateBadge(st protocol.State, tick int) string {
	switch st {
	case protocol.StateWorking:
		return styleStateWorking.Render(spinnerFrames[tick%len(spinnerFrames)] + " working")
	case protocol.StateNeedsInput:
		return styleStateNeeds.Render("◆ needs input")
	case protocol.StateDone:
		return styleStateDone.Render("● done")
	case protocol.StateFailed:
		return styleStateFailed.Render("✕ failed")
	case protocol.StateCrashed:
		return styleStateCrashed.Render("○ crashed")
	}
	return styleDim.Render(string(st))
}
