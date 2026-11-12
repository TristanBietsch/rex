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
	// BannerRows is the number of banner rows (0 or 1) between the title bar
	// and the rule above the agent's screen. Rows already excludes them:
	// attachPopupInnerSize(w, h, BannerRows) is the one source of geometry.
	BannerRows int
	// BannerSince is when the marquee loop last restarted: the popup opened,
	// or this session's description changed. Zero reads as just now.
	BannerSince time.Time
	// resize sends the agent's new size to the daemon. openAttach binds it to
	// the session's Client; tests replace it with a recorder.
	resize   func(cols, rows uint16) error
	Ended    bool
	EndedMsg string

	// outCh / errCh feed listenAttach. A background pump goroutine pulls
	// envelopes off the daemon connection so listenAttach can drain multiple
	// chunks per render — bursty agents like Gemini emit many small writes,
	// and re-rendering the popup on every chunk causes visible flicker.
	outCh chan AttachOutputMsg
	errCh chan AttachClosedMsg

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

	// Render caches (see attach_render.go). Only View touches them.
	paint    *glyphPainter
	rowCache []attachRowCache
	// bg is the board rendered behind the popup, kept across frames that
	// only bring agent output (see attachBackground).
	bg attachBgCache
}

// attachBgCache holds one rendered board background for a w×h screen.
type attachBgCache struct {
	w, h int
	s    string
	ok   bool
}

type AttachOutputMsg struct {
	SessionID string
	Bytes     []byte
}

type AttachClosedMsg struct {
	SessionID string
	Err       error
}

// altScreenTools are agents whose TUI lives entirely in the alt-screen and
// fully repaints on SIGWINCH (claude, codex, gemini, ...). For these we
// subscribe WITHOUT replay so vt10x doesn't have to chew through the entire
// transcript of alt-screen toggles, scroll-region resets, kitty-keyboard
// queries, and cursor positioning — which it doesn't fully emulate and which
// leaves duplicate / orphaned content on screen. Instead the daemon repaints
// its own screen mirror of the session into our fresh buffer
// (Subscribe.Repaint), so the popup shows the agent's current frame even
// when the agent doesn't draw again for a while.
//
// Line-oriented agents (ollama, echo) NEED the replay because they don't
// repaint scrollback on resize — the only way to see prior conversation is
// to replay the bytes.
var altScreenTools = map[string]bool{
	"claude":      true,
	"codex":       true,
	"antigravity": true,
	"gemini":      true,
	"grok":        true,
	"qwen":        true,
	"cursor":      true,
	"deepseek":    true,
	"kimi":        true,
}

// attachRepaintWait bounds how long the popup waits for a daemon that
// predates acks to answer its Subscribe before sending the ^L nudge (no
// replay comes first here). A variable so tests can stretch it.
var attachRepaintWait = client.RepaintReplyWait

func openAttach(m Model, sessionID string) (Model, tea.Cmd) {
	if m.Socket == "" {
		m.Err = "attach: socket unknown"
		return m, nil
	}
	c, err := client.Dial(m.Socket)
	if err != nil {
		slog.Warn("tui: attach dial failed", "session", sessionID, "err", err)
		m.Err = "attach: " + err.Error()
		return m, nil
	}
	chrome := attachChromeOf(m)
	banner := chrome.banner
	cols, rows := chrome.innerSize(m.Width, m.Height)
	toolID := ""
	for _, s := range m.Sessions {
		if s.ID == sessionID {
			toolID = s.ToolID
			break
		}
	}
	fullScreen := altScreenTools[toolID]
	// Size the PTY first: the daemon resizes its screen mirror with it, so
	// the repaint below has the popup's geometry. A real size change also
	// makes the agent redraw (SIGWINCH).
	resize := func(cols, rows uint16) error { return c.Resize(sessionID, cols, rows) }
	_ = resize(uint16(cols), uint16(rows)) //nolint:gosec // G115: attachChrome.innerSize is bounded by the terminal size, far below 65536.
	// Alt-screen agents skip replay and get a repaint of the daemon's
	// screen mirror instead; mode sync (for daemons without repaints) tells
	// our emulator which terminal modes they already switched on.
	reqID, subErr := c.SubscribeID(protocol.Subscribe{
		SessionID: sessionID, Replay: !fullScreen, ModeSync: true, Repaint: fullScreen,
	})
	if subErr != nil {
		slog.Warn("tui: attach subscribe failed", "session", sessionID, "err", subErr)
		_ = c.Close()
		m.Err = "attach: " + subErr.Error()
		return m, nil
	}
	var watch *client.RepaintWatch
	if fullScreen {
		// A daemon that cannot repaint leaves our fresh buffer empty, and
		// SIGWINCH alone is a no-op if the size didn't change: nudge the
		// agent with ^L (documented in our footer as "redraw"). Safe because
		// readline-based agents (ollama) aren't in altScreenTools.
		watch = client.WatchRepaint(sessionID, reqID, attachRepaintWait, func() {
			if err := c.SendRedraw(sessionID); err != nil {
				slog.Warn("tui: attach redraw nudge failed", "session", sessionID, "err", err)
				return
			}
			slog.Info("tui: attach redraw nudge sent; daemon did not repaint", "session", sessionID)
		})
	}
	slug := ""
	for _, s := range m.Sessions {
		if s.ID == sessionID {
			slug = s.Slug
			break
		}
	}
	mouse := mouseEnabled(m)
	scrollback := attachScrollbackLimit(m)
	slog.Info("tui: attach opened", "session", sessionID, "cols", cols, "rows", rows, "mouse", mouse, "mode_sync", true,
		"repaint", fullScreen, "scrollback_lines", scrollback)
	st := &AttachState{
		SessionID: sessionID, Slug: slug, Client: c, Cols: cols, Rows: rows, BannerRows: banner, resize: resize, BannerSince: m.now(),
		outCh: make(chan AttachOutputMsg, 64),
		errCh: make(chan AttachClosedMsg, 1),
	}
	st.Term = newAttachTerm(st, cols, rows, scrollback)
	go pumpAttach(c, sessionID, watch, st.outCh, st.errCh)
	m.Attach = st
	m.Focus = FocusAttach
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

// attachPopupInnerSize returns the agent's terminal size inside the desktop
// popup (not the mobile sheet; attachChromeOf(m).innerSize picks between
// them). bannerRows (0 or 1) is the banner row the popup shows above the
// rule; the agent loses that many rows.
func attachPopupInnerSize(termW, termH, bannerRows int) (cols, rows int) {
	return attachChrome{banner: bannerRows}.innerSize(termW, termH)
}

func resizeAttach(m Model) {
	if m.Attach == nil {
		return
	}
	chrome := attachChromeOf(m)
	banner := chrome.banner
	cols, rows := chrome.innerSize(m.Width, m.Height)
	if cols == m.Attach.Cols && rows == m.Attach.Rows && banner == m.Attach.BannerRows {
		return
	}
	slog.Info("tui: attach resize", "session", m.Attach.SessionID, "cols", cols, "rows", rows, "banner", banner)
	m.Attach.Cols, m.Attach.Rows, m.Attach.BannerRows = cols, rows, banner
	m.Attach.Term.Resize(cols, rows)
	// A shrink can push rows into scrollback; keep the offset in range.
	m.Attach.ScrollOff = min(m.Attach.ScrollOff, m.Attach.sb.Len())
	_ = m.Attach.resize(uint16(cols), uint16(rows)) //nolint:gosec // G115: attachChrome.innerSize is bounded by the terminal size, far below 65536.
}

// pumpAttach is the background reader goroutine. It blocks on the daemon
// connection and pushes parsed messages onto the AttachState channels until the
// connection closes. One pump per AttachState; closing the client unblocks it.
// watch (nil for replayed agents) sees every event, in order, to decide on
// the redraw nudge.
func pumpAttach(c *client.Client, sessionID string, watch *client.RepaintWatch, outCh chan<- AttachOutputMsg, errCh chan<- AttachClosedMsg) {
	defer watch.Stop()
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
		watch.Observe(env)
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
		// don't drop bytes. The TUI will drain on the next render.
		outCh <- AttachOutputMsg{SessionID: so.SessionID, Bytes: so.Bytes}
	}
}

// attachCoalesceWindow is how long we wait after the first chunk arrives
// before flushing as a single re-render. Caps re-renders at ~33fps. Gemini's
// "Thinking..." spinner can emit dozens of writes per second; without this
// pause each write triggered its own lipgloss render and the popup flickered.
const attachCoalesceWindow = 30 * time.Millisecond

// listenAttach blocks for the next chunk from the pump, waits a short window
// for more chunks to arrive, then merges everything into a single
// AttachOutputMsg so the popup re-renders at most once per window.
func listenAttach(st *AttachState) tea.Cmd {
	out := st.outCh
	errc := st.errCh
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
				}
			}
			if chunks > 1 {
				slog.Debug("tui: attach coalesced chunks", "session", sessionID, "chunks", chunks, "bytes", len(merged))
			}
			return AttachOutputMsg{SessionID: sessionID, Bytes: merged}
		case ec := <-errc:
			return ec
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
	b := append(prefix, keyToBytes(k)...) //nolint:gocritic // appendAssign false positive: prefix is a fresh copy and is not read again.
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
	lines := make([]string, 0, rows+4+a.BannerRows)
	// Chrome lines are clipped to the terminal width: a wider line would
	// widen the popup and shift it off the geometry mouse hit-testing assumes.
	lines = append(lines, ansi.Truncate(attachTitleBar(m, a, cols), cols, "…"))
	if a.BannerRows > 0 {
		lines = append(lines, renderAttachBanner(m, a, cols))
	}
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
		stateChip = styleMarker[protocol.StateFailed].Render("✕ ended")
	} else if haveSum {
		stateChip = attachStateChip(m, sum.State)
		if waitingKindOf(m, sum) != "" {
			stateChip = waitingChip(m, sum)
		}
	} else {
		stateChip = styleDim.Render("· unknown")
	}
	// Session strings are sanitized before styling (protocol.FlattenDisplayText strips
	// escapes, so it can't run on the styled line).
	title := protocol.FlattenDisplayText(a.Slug)
	if haveSum {
		title = displayName(sum)
		if sum.Name != "" {
			title += styleDim.Render(" · " + protocol.FlattenDisplayText(sum.Slug))
		}
	}
	slug := styleKey.Render(title)
	var meta string
	if haveSum {
		meta = styleDim.Render(protocol.FlattenDisplayText(sum.ToolID + " · " + modelLabel(sum)))
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
	switch st {
	case protocol.StateWorking:
		frames := spinnerFramesFor(m)
		g := frames[m.SpinnerTick%len(frames)]
		return styleMarker[protocol.StateWorking].Render(g + " working")
	case protocol.StateQueued:
		return styleMarker[protocol.StateQueued].Render(queuedGlyph + " queued")
	case protocol.StateNeedsInput:
		return styleMarker[protocol.StateNeedsInput].Render("◆ needs input")
	case protocol.StateDone:
		return styleMarker[protocol.StateDone].Render("● done")
	case protocol.StateFailed:
		return styleMarker[protocol.StateFailed].Render("✕ failed")
	case protocol.StateCrashed:
		return styleMarker[protocol.StateCrashed].Render("○ crashed")
	default:
		return styleDim.Render("· " + string(st))
	}
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
		msg := a.EndedMsg
		if msg == "" {
			msg = "session ended"
		}
		right = styleFailed.Render("✕ " + msg)
	}
	pad := width - lipglossWidth(left) - lipglossWidth(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func kbdChip(k string) string {
	return styleKbd.Render(k)
}

func findSession(sessions []protocol.SessionSummary, id string) (protocol.SessionSummary, bool) {
	for _, s := range sessions {
		if s.ID == id {
			return s, true
		}
	}
	return protocol.SessionSummary{}, false
}

func lipglossWidth(s string) int {
	return lipgloss.Width(s)
}

// detach closes the popup and re-syncs mouse capture for the board: it stays
// on when the board wants it (mouse_enabled) and is released otherwise.
func detach(m Model) (Model, tea.Cmd) {
	return syncMouse(closeAttach(m), "detach")
}
