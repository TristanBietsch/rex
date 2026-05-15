package tui

import (
	"encoding/json"
	"regexp"
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
	CmdMode   bool   // user pressed `:` — accumulating a command
	CmdText   string // current command text
}

// Popup size: a fixed inset from the terminal edges.
const (
	modalHInset = 8  // total horizontal inset (box ~w-8)
	modalVInset = 6  // total vertical inset (box ~h-6)
	modalMinW   = 60
	modalMinH   = 16
)

func modalBoxSize(w, h int) (int, int) {
	bw := w - modalHInset
	bh := h - modalVInset
	if bw < modalMinW {
		bw = modalMinW
	}
	if bh < modalMinH {
		bh = modalMinH
	}
	if bw > w {
		bw = w
	}
	if bh > h {
		bh = h
	}
	return bw, bh
}

// modalViewportSize: box dimensions minus border(2) minus padding(2) minus top/footer(4 lines).
func modalViewportSize(w, h int) (int, int) {
	bw, bh := modalBoxSize(w, h)
	vw := bw - 4 // border(2) + padding(2)
	vh := bh - 6 // border(2) + padding(2) + title(1) + footer(1)
	if vw < 20 {
		vw = 20
	}
	if vh < 4 {
		vh = 4
	}
	return vw, vh
}

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

func resizeModal(m Model, w, h int) Model {
	if m.Modal == nil {
		return m
	}
	vw, vh := modalViewportSize(w, h)
	m.Modal.Viewport.Width = vw
	m.Modal.Viewport.Height = vh
	m.Modal.Viewport.SetContent(modalDisplayText(m.Modal.Buffer.String(), vh))
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
	m.Modal.Viewport.SetContent(modalDisplayText(m.Modal.Buffer.String(), m.Modal.Viewport.Height))
	m.Modal.Viewport.GotoBottom()
	return m
}

// ANSI escape sequence regex (CSI, OSC, single-char, charset).
var ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[PX^_].*?\x1b\\|\x1b[()][AB012]|\x1b[78=>cMHE]`)

// modalDisplayText strips ANSI escapes and returns the last `lines` non-empty lines,
// padded with blank lines on top so content is bottom-aligned.
func modalDisplayText(raw string, lines int) string {
	if lines <= 0 {
		return ""
	}
	clean := ansiRegex.ReplaceAllString(raw, "")
	// Collapse CR (\r) — many TUIs use CR for cursor rewrites; treat as line reset.
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	all := strings.Split(clean, "\n")
	// Drop trailing empties.
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	// Pad with empties at top so output sits at bottom of viewport.
	for len(all) < lines {
		all = append([]string{""}, all...)
	}
	return strings.Join(all, "\n")
}

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

// renderModal returns the popup content (without the outer border / Place call —
// the View function handles centering with centerOverlay).
func renderModal(m Model) string {
	if m.Modal == nil {
		return ""
	}
	w, h := m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	bw, bh := modalBoxSize(w, h)
	innerW := bw - 4 // border + padding
	_ = bh

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
		meta += styleDim.Render(" · " + sess.Effort)
	}
	pill := stateBadge(sess.State, m.SpinnerTick)
	left := title + meta
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(pill)
	gap := innerW - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	topBar := left + strings.Repeat(" ", gap) + pill

	sep := styleBorderFg.Render(strings.Repeat("─", innerW))

	body := m.Modal.Viewport.View()

	// Footer: either the modal command line, or the static hint.
	var footer string
	if m.Modal.CmdMode {
		footer = styleArrowYellow.Render(":") + " " + stylePrimary.Render(m.Modal.CmdText) + cursorBlock(m)
	} else {
		footer = styleDim.Render("ctrl+] detach · :q quit · type to send input")
	}

	return strings.Join([]string{topBar, sep, body, sep, footer}, "\n")
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
