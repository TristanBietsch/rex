package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

type AttachState struct {
	SessionID string
	Slug      string
	Client    *client.Client
	Term      vt10x.Terminal
	Cols      int
	Rows      int
	Ended     bool
	EndedMsg  string
}

type AttachOutputMsg struct {
	SessionID string
	Bytes     []byte
}

type AttachClosedMsg struct {
	SessionID string
	Err       error
}

const detachKeyByte byte = 0x1d

func openAttach(m Model, sessionID string) (Model, tea.Cmd) {
	if m.Socket == "" {
		m.Err = "attach: socket unknown"
		return m, nil
	}
	c, err := client.Dial(m.Socket)
	if err != nil {
		m.Err = "attach: " + err.Error()
		return m, nil
	}
	cols, rows := attachPopupInnerSize(m.Width, m.Height)
	_ = c.Resize(sessionID, uint16(cols), uint16(rows))
	if err := c.SubscribeReplay(sessionID); err != nil {
		_ = c.Close()
		m.Err = "attach: " + err.Error()
		return m, nil
	}
	term := vt10x.New(vt10x.WithSize(cols, rows))
	slug := ""
	for _, s := range m.Sessions {
		if s.ID == sessionID {
			slug = s.Slug
			break
		}
	}
	m.Attach = &AttachState{SessionID: sessionID, Slug: slug, Client: c, Term: term, Cols: cols, Rows: rows}
	m.Focus = FocusAttach
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, tea.Batch(listenAttach(c, sessionID), tea.HideCursor)
}

func closeAttach(m Model) Model {
	if m.Attach != nil {
		if m.Attach.Client != nil {
			_ = m.Attach.Client.Close()
		}
		m.Attach = nil
	}
	m.Focus = FocusBoard
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

func attachPopupInnerSize(termW, termH int) (cols, rows int) {
	if termW <= 0 {
		termW = 100
	}
	if termH <= 0 {
		termH = 32
	}
	popupW := termW - 8
	popupH := termH - 4
	cols = popupW - 4
	rows = popupH - 4 - 4
	if cols < 40 {
		cols = 40
	}
	if rows < 8 {
		rows = 8
	}
	return cols, rows
}

func resizeAttach(m Model) {
	if m.Attach == nil {
		return
	}
	cols, rows := attachPopupInnerSize(m.Width, m.Height)
	if cols == m.Attach.Cols && rows == m.Attach.Rows {
		return
	}
	m.Attach.Cols, m.Attach.Rows = cols, rows
	m.Attach.Term.Resize(cols, rows)
	_ = m.Attach.Client.Resize(m.Attach.SessionID, uint16(cols), uint16(rows))
}

func listenAttach(c *client.Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		for {
			env, err := c.NextEvent()
			if err != nil {
				return AttachClosedMsg{SessionID: sessionID, Err: err}
			}
			if env.Type != protocol.EventSessionOutput {
				continue
			}
			var so protocol.SessionOutput
			if err := json.Unmarshal(env.Data, &so); err != nil {
				continue
			}
			if so.SessionID != sessionID {
				continue
			}
			return AttachOutputMsg{SessionID: so.SessionID, Bytes: so.Bytes}
		}
	}
}

func updateAttachKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Attach == nil {
		return m, nil
	}
	if k.Type == tea.KeyCtrlCloseBracket {
		return closeAttach(m), tea.ShowCursor
	}
	b := keyToBytes(k)
	if len(b) == 0 {
		return m, nil
	}
	if err := m.Attach.Client.SendInput(m.Attach.SessionID, b); err != nil {
		m.Err = "attach: " + err.Error()
	}
	return m, nil
}

func keyToBytes(k tea.KeyMsg) []byte {
	switch k.Type {
	case tea.KeyRunes:
		return []byte(string(k.Runes))
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyEnter:
		return []byte{0x0d}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte{0x09}
	case tea.KeyShiftTab:
		return []byte("\x1b[Z")
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyRight:
		return []byte("\x1b[C")
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
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlH:
		return []byte{0x08}
	case tea.KeyCtrlJ:
		return []byte{0x0a}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlQ:
		return []byte{0x11}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlV:
		return []byte{0x16}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlX:
		return []byte{0x18}
	case tea.KeyCtrlY:
		return []byte{0x19}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeyCtrlBackslash:
		return []byte{0x1c}
	case tea.KeyCtrlCaret:
		return []byte{0x1e}
	case tea.KeyCtrlUnderscore:
		return []byte{0x1f}
	}
	return nil
}

func renderAttach(m Model) string {
	if m.Attach == nil {
		return ""
	}
	a := m.Attach
	a.Term.Lock()
	defer a.Term.Unlock()
	cols, rows := a.Term.Size()
	cur := a.Term.Cursor()
	cursorVisible := a.Term.CursorVisible()
	lines := make([]string, 0, rows+4)
	lines = append(lines, attachTitleBar(m, a, cols))
	lines = append(lines, styleBorderFg.Render(strings.Repeat("─", cols)))
	for y := 0; y < rows; y++ {
		var row strings.Builder
		var run strings.Builder
		var runStyle lipgloss.Style
		var haveStyle bool
		flush := func() {
			if run.Len() == 0 {
				return
			}
			if haveStyle {
				row.WriteString(runStyle.Render(run.String()))
			} else {
				row.WriteString(run.String())
			}
			run.Reset()
		}
		for x := 0; x < cols; x++ {
			g := a.Term.Cell(x, y)
			r := g.Char
			if r == 0 || r == ' ' {
				r = ' '
			}
			isCursor := cursorVisible && cur.X == x && cur.Y == y
			style := glyphStyle(g)
			if isCursor {
				style = style.Reverse(true)
			}
			if !haveStyle || !styleEq(style, runStyle) {
				flush()
				runStyle = style
				haveStyle = true
			}
			run.WriteRune(r)
		}
		flush()
		lines = append(lines, row.String())
	}
	lines = append(lines, styleBorderFg.Render(strings.Repeat("─", cols)))
	lines = append(lines, attachFooter(a, cols))
	return strings.Join(lines, "\n")
}

func attachTitleBar(m Model, a *AttachState, width int) string {
	sum, haveSum := findSession(m.Sessions, a.SessionID)
	var stateChip string
	if a.Ended {
		stateChip = lipgloss.NewStyle().Bold(true).Foreground(colorFailed).Render("✕ ended")
	} else if haveSum {
		stateChip = attachStateChip(m, sum.State)
	} else {
		stateChip = styleDim.Render("· unknown")
	}
	slug := styleHeaderApp.Render(a.Slug)
	var meta string
	if haveSum {
		meta = styleDim.Render(sum.ToolID + " · " + modelLabel(sum))
	}
	geom := styleMuted.Render(fmt.Sprintf("%d×%d", a.Cols, a.Rows))
	left := stateChip + "  " + slug
	right := geom
	if meta != "" {
		right = meta + "   " + geom
	}
	pad := width - lipglossWidth(left) - lipglossWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func attachStateChip(m Model, st protocol.State) string {
	bold := lipgloss.NewStyle().Bold(true)
	switch st {
	case protocol.StateWorking:
		frames := spinnerFramesFor(m)
		g := frames[m.SpinnerTick%len(frames)]
		return bold.Foreground(colorWorking).Render(g + " working")
	case protocol.StateNeedsInput:
		return bold.Foreground(colorNeeds).Render("◆ needs input")
	case protocol.StateDone:
		return bold.Foreground(colorDone).Render("● done")
	case protocol.StateFailed:
		return bold.Foreground(colorFailed).Render("✕ failed")
	case protocol.StateCrashed:
		return bold.Foreground(colorCrashed).Render("○ crashed")
	}
	return styleDim.Render("· " + string(st))
}

func attachFooter(a *AttachState, width int) string {
	sep := styleMuted.Render("  ·  ")
	left := kbdChip("^]") + styleDim.Render(" detach") + sep +
		kbdChip("^L") + styleDim.Render(" redraw") + sep +
		kbdChip("^C") + styleDim.Render(" interrupt")
	var right string
	if a.Ended {
		msg := a.EndedMsg
		if msg == "" {
			msg = "session ended"
		}
		right = lipgloss.NewStyle().Foreground(colorFailed).Render("✕ " + msg)
	}
	pad := width - lipglossWidth(left) - lipglossWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func kbdChip(k string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(colorWorking).Render(k)
}

func findSession(sessions []protocol.SessionSummary, id string) (protocol.SessionSummary, bool) {
	for _, s := range sessions {
		if s.ID == id {
			return s, true
		}
	}
	return protocol.SessionSummary{}, false
}

func glyphStyle(g vt10x.Glyph) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c, ok := vtFGColor(g.FG); ok {
		s = s.Foreground(c)
	}
	if c, ok := vtBGColor(g.BG); ok {
		s = s.Background(c)
	}
	return s
}

func vtFGColor(c vt10x.Color) (lipgloss.Color, bool) {
	if c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor {
		return "", false
	}
	return lipgloss.Color(fmt.Sprintf("%d", uint32(c))), true
}

func vtBGColor(c vt10x.Color) (lipgloss.Color, bool) {
	if c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor {
		return "", false
	}
	return lipgloss.Color(fmt.Sprintf("%d", uint32(c))), true
}

func styleEq(a, b lipgloss.Style) bool {
	return a.GetForeground() == b.GetForeground() &&
		a.GetBackground() == b.GetBackground() &&
		a.GetReverse() == b.GetReverse()
}

func lipglossWidth(s string) int {
	return lipgloss.Width(s)
}
