package tui

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"
)

// Mouse support inside the attach popup. The popup shares the board's mouse
// capture (see syncMouse in board_mouse.go). Events over the popup's
// terminal area are delivered to the agent the way a real terminal would: as
// mouse reports when the agent enabled mouse tracking (Claude Code fullscreen
// scrolls its own history on wheel reports), else the wheel becomes arrow
// keys on the alt screen — what terminals do natively.
// On the primary screen without tracking, wheel up opens rex's local
// scrollback (attach_scroll.go).

// attachWheelFlushWindow batches a trackpad flick's many wheel events into
// one SendInput (the daemon's per-session input queue holds only 16 writes).
const attachWheelFlushWindow = 16 * time.Millisecond

// attachWheelLinesPerNotch is how many arrow presses one wheel notch sends
// when the agent has no mouse tracking.
const attachWheelLinesPerNotch = 3

type attachWheelFlushMsg struct {
	SessionID string
	Seq       int
}

func mouseEnabled(m Model) bool {
	if m.Store == nil {
		return true
	}
	if v, ok := m.Store.Get("mouse_enabled").(bool); ok {
		return v
	}
	return true
}

// attachInnerOrigin mirrors centerOverlay's placement of renderAttach:
// border (1) + padding (2 cols / 1 row), then a title bar and a rule above
// the terminal rows. Returns the 0-based screen cell of the terminal's
// top-left cell.
func attachInnerOrigin(m Model) (x, y int) {
	w, h := m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	cols, rows := m.Attach.Cols, m.Attach.Rows
	popupW := cols + 2 + 4
	popupH := rows + 4 + 2 + 2
	ox := (w - popupW) / 2
	if ox < 0 {
		ox = 0
	}
	oy := (h - popupH) / 2
	if oy < 0 {
		oy = 0
	}
	return ox + 1 + 2, oy + 1 + 1 + 2
}

// updateAttachMouse routes a mouse event that arrived while attached.
func updateAttachMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	a := m.Attach
	if a == nil || !mouseEnabled(m) {
		return m, nil
	}
	ox, oy := attachInnerOrigin(m)
	cx, cy := msg.X-ox, msg.Y-oy
	if cx < 0 || cy < 0 || cx >= a.Cols || cy >= a.Rows {
		return m, nil
	}
	a.Term.Lock()
	mode := a.Term.Mode()
	a.Term.Unlock()

	if tea.MouseEvent(msg).IsWheel() {
		if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
			return m, nil // horizontal wheel: no sensible mapping
		}
		up := msg.Button == tea.MouseButtonWheelUp
		if a.Scrolling() {
			// Scroll mode owns the wheel until it returns to live output.
			delta := attachScrollWheelLines
			if !up {
				delta = -delta
			}
			scrollAttach(a, delta, "wheel")
			return m, nil
		}
		if a.Ended {
			// Nobody to forward to, but the history is still readable.
			if up && mode&vt10x.ModeAltScreen == 0 {
				scrollAttach(a, attachScrollWheelLines, "wheel")
			}
			return m, nil
		}
		var seq []byte
		switch {
		case mode&vt10x.ModeMouseMask != 0:
			seq = encodeMouse(msg, cx, cy, mode)
		case mode&vt10x.ModeAltScreen != 0:
			seq = wheelAsArrows(up, mode)
		default:
			// Primary screen without tracking: wheel up scrolls our local
			// scrollback (what a real terminal does).
			if up {
				scrollAttach(a, attachScrollWheelLines, "wheel")
			}
			return m, nil
		}
		if len(seq) == 0 {
			return m, nil
		}
		a.wheelBuf = append(a.wheelBuf, seq...)
		a.wheelNotches++
		if a.wheelFlushPending {
			return m, nil
		}
		a.wheelFlushPending = true
		a.wheelSeq++
		id, s := a.SessionID, a.wheelSeq
		return m, tea.Tick(attachWheelFlushWindow, func(time.Time) tea.Msg {
			return attachWheelFlushMsg{SessionID: id, Seq: s}
		})
	}

	// Clicks and drags only matter to live agents that asked for them.
	if a.Ended || mode&vt10x.ModeMouseMask == 0 {
		return m, nil
	}
	if msg.Action == tea.MouseActionMotion && mode&(vt10x.ModeMouseMotion|vt10x.ModeMouseMany) == 0 {
		return m, nil
	}
	if msg.Action == tea.MouseActionRelease && mode&vt10x.ModeMouseX10 != 0 {
		return m, nil
	}
	if seq := encodeMouse(msg, cx, cy, mode); len(seq) > 0 {
		if err := a.Client.SendInput(a.SessionID, seq); err != nil {
			m.Err = "attach: " + err.Error()
		}
	}
	return m, nil
}

// flushAttachWheel sends the batched wheel input.
func flushAttachWheel(m Model, msg attachWheelFlushMsg) (Model, tea.Cmd) {
	a := m.Attach
	if a == nil || a.SessionID != msg.SessionID || a.wheelSeq != msg.Seq {
		return m, nil
	}
	buf, n := a.wheelBuf, a.wheelNotches
	a.wheelBuf, a.wheelNotches, a.wheelFlushPending = nil, 0, false
	if len(buf) == 0 || a.Ended {
		return m, nil
	}
	slog.Debug("tui: wheel forwarded", "session", a.SessionID, "notches", n, "bytes", len(buf))
	if err := a.Client.SendInput(a.SessionID, buf); err != nil {
		m.Err = "attach: " + err.Error()
	}
	return m, nil
}

func wheelAsArrows(up bool, mode vt10x.ModeFlag) []byte {
	key := "\x1b[B"
	if up {
		key = "\x1b[A"
	}
	if mode&vt10x.ModeAppCursor != 0 {
		key = "\x1bO" + key[2:]
	}
	var out []byte
	for i := 0; i < attachWheelLinesPerNotch; i++ {
		out = append(out, key...)
	}
	return out
}

// encodeMouse renders a mouse event as an xterm report at 0-based cell
// (cx, cy): SGR (CSI < b ; x ; y M/m) when the agent enabled 1006, else the
// legacy X10 form (CSI M b x y), which cannot address cells past 223.
func encodeMouse(msg tea.MouseMsg, cx, cy int, mode vt10x.ModeFlag) []byte {
	var b int
	switch msg.Button {
	case tea.MouseButtonLeft:
		b = 0
	case tea.MouseButtonMiddle:
		b = 1
	case tea.MouseButtonRight:
		b = 2
	case tea.MouseButtonNone:
		b = 3
	case tea.MouseButtonWheelUp:
		b = 64
	case tea.MouseButtonWheelDown:
		b = 65
	default:
		return nil
	}
	if msg.Action == tea.MouseActionMotion {
		b += 32
	}
	if msg.Shift {
		b += 4
	}
	if msg.Alt {
		b += 8
	}
	if msg.Ctrl {
		b += 16
	}
	x, y := cx+1, cy+1
	if mode&vt10x.ModeMouseSgr != 0 {
		final := 'M'
		if msg.Action == tea.MouseActionRelease {
			final = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", b, x, y, final))
	}
	if msg.Action == tea.MouseActionRelease {
		b = 3 | (b &^ 3 &^ 64) // legacy release has no button identity
	}
	if x > 223 || y > 223 {
		return nil
	}
	return []byte{0x1b, '[', 'M', byte(32 + b), byte(32 + x), byte(32 + y)}
}
