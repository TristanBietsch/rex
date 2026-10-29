package tui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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

	// outCh / errCh feed listenAttach. A background pump goroutine pulls
	// envelopes off the daemon connection so listenAttach can drain multiple
	// chunks per render — bursty agents like Gemini emit many small writes,
	// and re-rendering the popup on every chunk causes visible flicker.
	outCh chan AttachOutputMsg
	errCh chan AttachClosedMsg
	// done is closed by closeAttach so a pump blocked on a full outCh
	// (nobody drains it after detach) exits instead of leaking.
	done chan struct{}

	// held is a lone ':' or 'q' waiting to see if its twin follows ("::"
	// opens rex's command bar, "qq" detaches); 0 when nothing is held.
	// holdSeq invalidates stale flush timers.
	held    rune
	holdSeq int

	// Batched wheel input awaiting its flush tick (see attach_mouse.go).
	wheelBuf          []byte
	wheelNotches      int
	wheelFlushPending bool
	wheelSeq          int

	// Local scrollback (see attach_scroll.go). ScrollOff > 0 is scroll mode:
	// the view sits that many lines above live output. scrollNew counts lines
	// that arrived while scrolled up.
	sb        *attachScrollback
	ScrollOff int
	scrollNew int
}

type AttachOutputMsg struct {
	SessionID string
	Bytes     []byte
	// from is the popup whose listener produced the message (nil when built
	// elsewhere); a reopened popup on the same session is a different one.
	from *AttachState
}

type AttachClosedMsg struct {
	SessionID string
	Err       error
	from      *AttachState
}

// attachMsgCurrent reports whether a listener message belongs to the open
// popup m.Attach rather than to one closed earlier.
func attachMsgCurrent(m Model, sessionID string, from *AttachState) bool {
	return m.Attach != nil && sessionID == m.Attach.SessionID && (from == nil || from == m.Attach)
}

// altScreenTools are agents whose TUI lives entirely in the alt-screen and
// fully repaints on SIGWINCH (claude, codex, gemini, ...). For these we
// subscribe WITHOUT replay so vt10x doesn't have to chew through the entire
// transcript of alt-screen toggles, scroll-region resets, kitty-keyboard
// queries, and cursor positioning — which it doesn't fully emulate and which
// leaves duplicate / orphaned content on screen. The resize that follows
// triggers the agent to redraw fresh into a clean buffer.
//
// Line-oriented agents (ollama, echo) NEED the replay because they don't
// repaint scrollback on resize — the only way to see prior conversation is
// to replay the bytes.
var altScreenTools = map[string]bool{
	"claude":   true,
	"codex":    true,
	"gemini":   true,
	"grok":     true,
	"deepseek": true,
	"kimi":     true,
}

// attachDialedMsg carries a popup connection opened by attachDialCmd: dialed,
// subscribed and sized, or the error that stopped it.
type attachDialedMsg struct {
	SessionID  string
	C          *client.Client
	Cols, Rows int
	Err        error
}

// openAttach starts opening the popup for sessionID. The daemon round trips
// (dial, subscribe, resize, redraw) run in attachDialCmd, off the event loop,
// so a slow daemon can't freeze input; attachOpened installs the popup.
func openAttach(m Model, sessionID string) (Model, tea.Cmd) {
	if m.Socket == "" {
		m.Err = "attach: socket unknown"
		return m, nil
	}
	cols, rows := attachPopupInnerSize(m.Width, m.Height)
	toolID := ""
	if s, ok := findSession(m.Sessions, sessionID); ok {
		toolID = s.ToolID
	}
	return m, attachDialCmd(m.Socket, sessionID, cols, rows, altScreenTools[toolID])
}

func attachDialCmd(socket, sessionID string, cols, rows int, altScreen bool) tea.Cmd {
	return func() tea.Msg {
		c, err := client.Dial(socket)
		if err != nil {
			slog.Warn("tui: attach dial failed", "session", sessionID, "err", err)
			return attachDialedMsg{SessionID: sessionID, Err: err}
		}
		// Alt-screen agents skip replay; mode sync tells our emulator which
		// terminal modes (mouse tracking, alt screen) they already switched on.
		if err := c.SubscribeOpts(sessionID, !altScreen, true); err != nil {
			slog.Warn("tui: attach subscribe failed", "session", sessionID, "err", err)
			_ = c.Close()
			return attachDialedMsg{SessionID: sessionID, Err: err}
		}
		_ = c.Resize(sessionID, uint16(cols), uint16(rows))
		if altScreen {
			// SIGWINCH alone is a no-op if size didn't change. Nudge full-TUI
			// agents with ^L (documented in our footer as "redraw") so they
			// repaint cleanly into our fresh vt10x buffer. Safe here because
			// readline-based agents (ollama) aren't in altScreenTools.
			_ = c.SendRedraw(sessionID)
		}
		return attachDialedMsg{SessionID: sessionID, C: c, Cols: cols, Rows: rows}
	}
}

// attachOpened installs the popup once its connection is ready. If the user
// moved on meanwhile (another popup, an overlay) the connection is dropped.
func attachOpened(m Model, msg attachDialedMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.Err = "attach: " + msg.Err.Error()
		return m, nil
	}
	if m.Attach != nil || m.Focus != FocusBoard || m.Quitting {
		slog.Info("tui: attach dropped, focus moved on", "session", msg.SessionID, "focus", m.Focus)
		_ = msg.C.Close()
		return m, nil
	}
	sessionID, c, cols, rows := msg.SessionID, msg.C, msg.Cols, msg.Rows
	slug := ""
	if s, ok := findSession(m.Sessions, sessionID); ok {
		slug = s.Slug
	}
	mouse := mouseEnabled(m)
	scrollback := attachScrollbackLimit(m)
	slog.Info("tui: attach opened", "session", sessionID, "cols", cols, "rows", rows, "mouse", mouse, "mode_sync", true,
		"scrollback_lines", scrollback)
	st := &AttachState{
		SessionID: sessionID, Slug: slug, Client: c, Cols: cols, Rows: rows,
		outCh: make(chan AttachOutputMsg, 64),
		errCh: make(chan AttachClosedMsg, 1),
		done:  make(chan struct{}),
	}
	st.Term = newAttachTerm(st, cols, rows, scrollback)
	go pumpAttach(c, sessionID, st.outCh, st.errCh, st.done)
	m.Attach = st
	m.Focus = FocusAttach
	// The window may have been resized while the dial was in flight.
	resizeAttach(m)
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	// The popup shares the board's mouse capture; this only enables it if
	// the board somehow doesn't hold it already.
	m, mc := syncMouse(m, "attach")
	return m, tea.Batch(listenAttach(st), tea.HideCursor, mc)
}

// closeAttach tears the popup down. Key handlers use detach, which also
// re-syncs mouse capture for the board.
func closeAttach(m Model) Model {
	if m.Attach != nil {
		slog.Info("tui: attach closed", "session", m.Attach.SessionID)
		logAttachScrollbackTotals(m.Attach)
		if m.Attach.done != nil {
			close(m.Attach.done)
		}
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
	// lipgloss border+padding eats 6 cells horizontally and 4 lines vertically;
	// renderAttach adds title+hr+hr+footer (4 more lines) on top.
	popupW := termW - 8
	popupH := termH - 4
	cols = popupW - 6
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
	slog.Info("tui: attach resize", "session", m.Attach.SessionID, "cols", cols, "rows", rows)
	m.Attach.Cols, m.Attach.Rows = cols, rows
	m.Attach.Term.Resize(cols, rows)
	// A shrink can push rows into scrollback; keep the offset in range.
	m.Attach.ScrollOff = min(m.Attach.ScrollOff, m.Attach.sb.Len())
	_ = m.Attach.Client.Resize(m.Attach.SessionID, uint16(cols), uint16(rows))
}

// pumpAttach is the background reader goroutine. It blocks on the daemon
// connection and pushes parsed messages onto the AttachState channels until the
// connection closes or done is closed. One pump per AttachState; closeAttach
// closes done and the client, which unblocks it wherever it waits.
func pumpAttach(c *client.Client, sessionID string, outCh chan<- AttachOutputMsg, errCh chan<- AttachClosedMsg, done <-chan struct{}) {
	for {
		env, err := c.NextEvent()
		if err != nil {
			// Non-blocking send: if nobody's listening (popup already closed)
			// drop the error rather than leaking the goroutine.
			select {
			case errCh <- AttachClosedMsg{SessionID: sessionID, Err: err}:
			default:
			}
			slog.Debug("tui: attach pump exit", "session", sessionID, "err", err)
			return
		}
		if env.Type != protocol.EventSessionOutput {
			continue
		}
		var so protocol.SessionOutput
		if err := json.Unmarshal(env.Data, &so); err != nil {
			slog.Warn("tui: attach decode output failed", "session", sessionID, "err", err)
			continue
		}
		if so.SessionID != sessionID {
			continue
		}
		// Buffered channel — if full (TUI fell behind), block briefly so we
		// don't drop bytes. The TUI will drain on the next render; after a
		// detach nobody will, so done ends the wait.
		select {
		case outCh <- AttachOutputMsg{SessionID: so.SessionID, Bytes: so.Bytes}:
		case <-done:
			slog.Debug("tui: attach pump cancelled", "session", sessionID)
			return
		}
	}
}

// attachCoalesceWindow is how long we wait after the first chunk arrives
// before flushing as a single re-render. Caps re-renders at ~33fps. Gemini's
// "Thinking..." spinner can emit dozens of writes per second; without this
// pause each write triggered its own lipgloss render and the popup flickered.
const attachCoalesceWindow = 30 * time.Millisecond

// listenAttach blocks for the next chunk from the pump, waits a short window
// for more chunks to arrive, then merges everything into a single
// AttachOutputMsg so the popup re-renders at most once per window. Once the
// popup closes (done) it returns nil, ending the chain.
func listenAttach(st *AttachState) tea.Cmd {
	out := st.outCh
	errc := st.errCh
	done := st.done
	sessionID := st.SessionID
	return func() tea.Msg {
		select {
		case msg := <-out:
			merged := msg.Bytes
			chunks := 1
			deadline := time.After(attachCoalesceWindow)
		coalesce:
			for {
				select {
				case more := <-out:
					merged = append(merged, more.Bytes...)
					chunks++
				case <-deadline:
					break coalesce
				case <-done:
					return nil
				}
			}
			if chunks > 1 {
				slog.Debug("tui: attach coalesced chunks", "session", sessionID, "chunks", chunks, "bytes", len(merged))
			}
			return AttachOutputMsg{SessionID: sessionID, Bytes: merged, from: st}
		case ec := <-errc:
			ec.from = st
			return ec
		case <-done:
			return nil
		}
	}
}

func updateAttachKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Attach == nil {
		return m, nil
	}
	if k.Type == tea.KeyCtrlCloseBracket {
		return detach(m)
	}
	if m.Attach.Scrolling() {
		return updateAttachScrollKey(m, k)
	}
	if k.Type == tea.KeyCtrlPgUp && m.Attach.sb.Len() > 0 && m.Attach.onPrimaryScreen() {
		scrollAttach(m.Attach, max(1, m.Attach.Rows-1), "ctrl+pgup")
		return m, nil
	}
	m, cmd, handled, prefix := interceptAttachHold(m, k, true)
	if handled {
		if len(prefix) > 0 && m.Attach != nil {
			if err := m.Attach.Client.SendInput(m.Attach.SessionID, prefix); err != nil {
				m.Err = "attach: " + err.Error()
			}
		}
		return m, cmd
	}
	b := append(prefix, keyToBytes(k)...)
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
	// Chrome lines are clipped to the terminal width: a wider line would
	// widen the popup and shift it off the geometry mouse hit-testing assumes.
	lines = append(lines, ansi.Truncate(attachTitleBar(m, a, cols), cols, "…"))
	lines = append(lines, styleBorderFg.Render(strings.Repeat("─", cols)))
	for y := 0; y < rows; y++ {
		cx := -1
		if cursorVisible && cur.Y == y {
			cx = cur.X
		}
		lines = append(lines, attachViewRow(a, y, cols, cx))
	}
	lines = append(lines, styleBorderFg.Render(strings.Repeat("─", cols)))
	if m.Focus == FocusCommand {
		lines = append(lines, ansi.Truncate(renderAttachCommandBar(m, cols), cols, "…"))
	} else if a.Scrolling() {
		lines = append(lines, ansi.Truncate(attachScrollFooter(a, cols), cols, "…"))
	} else {
		lines = append(lines, ansi.Truncate(attachFooter(m, a, cols), cols, "…"))
	}
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
	// Session text is untrusted: one row, or the popup grows and shifts off
	// the geometry mouse hit-testing assumes.
	title := protocol.SingleLine(a.Slug)
	if haveSum {
		title = protocol.SingleLine(displayName(sum))
		if sum.Name != "" {
			title += styleDim.Render(" · " + protocol.SingleLine(sum.Slug))
		}
	}
	slug := styleHeaderApp.Render(title)
	var meta string
	if haveSum {
		meta = styleDim.Render(protocol.SingleLine(sum.ToolID + " · " + modelLabel(sum)))
	}
	geom := styleMuted.Render(fmt.Sprintf("%d×%d", a.Cols, a.Rows))
	if a.Scrolling() {
		geom = attachScrollChip(a)
	}
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

func attachFooter(m Model, a *AttachState, width int) string {
	sep := styleMuted.Render("  ·  ")
	left := kbdChip("qq") + styleDim.Render(" detach") + sep +
		kbdChip("::") + styleDim.Render(" command") + sep +
		kbdChip("^L") + styleDim.Render(" redraw") + sep +
		kbdChip("^C") + styleDim.Render(" interrupt")
	if a.sb.Len() > 0 {
		left += sep + kbdChip("^PgUp") + styleDim.Render(" scroll")
	}
	var right string
	if sum, ok := findSession(m.Sessions, a.SessionID); ok && !a.Ended {
		if lbl := retireLabel(time.Now(), sum); lbl != "" {
			right = styleDim.Render(lbl)
		}
	}
	if a.Ended {
		msg := protocol.SingleLine(a.EndedMsg) // may wrap a multi-line error
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

// glyphStyle maps an emulator cell to a style. vt10x applies SGR 7 by
// swapping the cell's FG and BG, which is a no-op on default colors (both
// "default"), so reverse video vanished: undo the swap and let the terminal
// reverse instead.
func glyphStyle(g vt10x.Glyph) lipgloss.Style {
	fg, bg := g.FG, g.BG
	reverse := g.Mode&vt10x.AttrReverse != 0
	if reverse {
		fg, bg = bg, fg
	}
	s := lipgloss.NewStyle().
		Reverse(reverse).
		Bold(g.Mode&vt10x.AttrBold != 0).
		Italic(g.Mode&vt10x.AttrItalic != 0).
		Underline(g.Mode&vt10x.AttrUnderline != 0)
	if c, ok := vtFGColor(fg); ok {
		s = s.Foreground(c)
	}
	if c, ok := vtBGColor(bg); ok {
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
		a.GetReverse() == b.GetReverse() &&
		a.GetBold() == b.GetBold() &&
		a.GetItalic() == b.GetItalic() &&
		a.GetUnderline() == b.GetUnderline()
}

func lipglossWidth(s string) int {
	return lipgloss.Width(s)
}

// detach closes the popup and re-syncs mouse capture for the board: it stays
// on when the board wants it (mouse_enabled) and is released otherwise.
func detach(m Model) (Model, tea.Cmd) {
	return syncMouse(closeAttach(m), "detach")
}
