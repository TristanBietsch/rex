package tui

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// DaemonEventMsg wraps a single event from the daemon.
type DaemonEventMsg struct {
	Env protocol.Envelope
}

// DaemonErrMsg is sent when a read from the daemon connection fails: the
// connection died (see daemonConnDead) or one line didn't decode.
type DaemonErrMsg struct {
	Err error
}

// CmdErrMsg reports a failed one-shot daemon request (spawn, delete, rename…).
// Unlike DaemonErrMsg it must not re-arm the event listener: the listener is
// still running, and a second one would race it for events.
type CmdErrMsg struct {
	Op  string
	Err error
}

// dropLastRune removes the final character (not byte) of s.
func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// SpinnerTickMsg fires periodically to drive the working-state spinner.
// Gen tags the tick chain; only the current chain re-arms, so arming from
// several places can never stack up parallel chains (double-speed spinner).
type SpinnerTickMsg struct{ Gen int }

// listenDaemon returns a tea.Cmd that reads ONE event then re-arms itself.
func listenDaemon(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		env, err := c.NextEvent()
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return DaemonEventMsg{Env: env}
	}
}

// daemonConnDead reports whether a NextEvent error means the connection is
// gone (EOF, closed, reset) rather than one undecodable line on a live
// stream. A dead connection fails every read instantly, so re-arming the
// listener on it would spin Update and View at full CPU.
func daemonConnDead(err error) bool {
	var opErr *net.OpError
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.As(err, &opErr)
}

// daemonDownStatus is the status line while the board is disconnected.
const daemonDownStatus = "daemon disconnected · reconnecting…"

// Redial backoff: redialBase doubles per failed attempt up to redialMax.
// redialHandshakeTimeout bounds Hello so a wedged daemon can't stall the chain.
const (
	redialBase             = 250 * time.Millisecond
	redialMax              = 5 * time.Second
	redialHandshakeTimeout = 3 * time.Second
)

// daemonRedialMsg fires when the backoff for redial attempt Attempt expires.
type daemonRedialMsg struct{ Attempt int }

// daemonRedialResultMsg carries a redial's outcome: on success a connected,
// subscribed client and the fresh snapshot.
type daemonRedialResultMsg struct {
	C       *client.Client
	Snap    *protocol.Snapshot
	Err     error
	Attempt int
}

// redialDelay is the backoff before redial attempt n (0-based).
func redialDelay(attempt int) time.Duration {
	return min(redialBase<<min(attempt, 5), redialMax)
}

// scheduleRedial waits out the backoff for attempt, then asks Update to redial.
func scheduleRedial(attempt int) tea.Cmd {
	return tea.Tick(redialDelay(attempt), func(time.Time) tea.Msg { return daemonRedialMsg{Attempt: attempt} })
}

// redialDaemonCmd repeats boot's client.dial → handshake → subscribe steps
// off the event loop.
func redialDaemonCmd(socket string, attempt int) tea.Cmd {
	return func() tea.Msg {
		c, err := client.Dial(socket)
		if err != nil {
			return daemonRedialResultMsg{Err: err, Attempt: attempt}
		}
		_ = c.SetReadDeadline(time.Now().Add(redialHandshakeTimeout))
		snap, err := c.Hello("rex-tui")
		if err == nil {
			err = c.SetReadDeadline(time.Time{})
		}
		if err == nil {
			err = c.Subscribe("")
		}
		if err != nil {
			_ = c.Close()
			return daemonRedialResultMsg{Err: err, Attempt: attempt}
		}
		return daemonRedialResultMsg{C: c, Snap: snap, Attempt: attempt}
	}
}

// daemonLost handles a dead board connection: stop listening on it, say so
// on the status line, and start the redial chain. m.Client stays set (closed)
// so requests made meanwhile fail with an error instead of a nil client.
func daemonLost(m Model, err error) (Model, tea.Cmd) {
	slog.Warn("tui: daemon connection lost", "err", err)
	if m.Client != nil {
		_ = m.Client.Close()
	}
	m.Err = daemonDownStatus
	if m.Socket == "" {
		return m, nil
	}
	return m, scheduleRedial(0)
}

// daemonRedialed applies a redial result: on failure back off and retry, on
// success swap in the new client and its snapshot and listen again. The tool
// filter is local UI state and is kept as is.
func daemonRedialed(m Model, msg daemonRedialResultMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		slog.Debug("tui: daemon redial failed", "attempt", msg.Attempt, "err", msg.Err)
		m.Err = daemonDownStatus
		return m, scheduleRedial(msg.Attempt + 1)
	}
	slog.Info("tui: daemon reconnected", "attempt", msg.Attempt, "sessions", len(msg.Snap.Sessions))
	m.Client = msg.C
	m.Sessions = msg.Snap.Sessions
	if m.Err == daemonDownStatus {
		m.Err = ""
	}
	m.Notice = "reconnected to daemon"
	return ensureVisible(m), listenDaemon(m.Client)
}

// tickSpinner emits a SpinnerTickMsg for chain gen after 100ms.
func tickSpinner(gen int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return SpinnerTickMsg{Gen: gen} })
}

// armSpinner starts a new spinner tick chain when something animates and no
// chain is running. The chain stops itself once nothing animates, so Update
// re-arms it after every message — before this, the spinner froze for good
// after the first idle moment.
func armSpinner(m Model) (Model, tea.Cmd) {
	if m.spinnerRunning || !m.wantsSpinnerTick() {
		return m, nil
	}
	m.spinnerGen++
	m.spinnerRunning = true
	slog.Debug("tui: spinner armed", "gen", m.spinnerGen, "working", m.hasWorkingSessions(), "focus", m.Focus)
	return m, tickSpinner(m.spinnerGen)
}

// wantsSpinnerTick reports whether anything on screen is driven by the
// SpinnerTick chain: working-row / attach-chip spinners, the blinking text
// cursor of an open input bar, and the done-row flash (which reads the clock
// at render time and so needs periodic re-renders).
func (m Model) wantsSpinnerTick() bool {
	if m.hasWorkingSessions() {
		return true
	}
	if m.cursorBlinking() {
		return true
	}
	return m.doneBlinkActive(time.Now())
}

// cursorBlinking is true while an input bar showing cursorBlock is focused.
func (m Model) cursorBlinking() bool {
	switch m.Focus {
	case FocusPrompt, FocusCommand, FocusRename, FocusWizard:
	default:
		return false
	}
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return false // steady cursor
		}
	}
	return true
}

// pruneDoneBlinks drops expired done-row flashes so BlinkUntil doesn't grow
// for the life of the TUI.
func pruneDoneBlinks(m Model, now time.Time) Model {
	for id, until := range m.BlinkUntil {
		if !now.Before(until) {
			delete(m.BlinkUntil, id)
		}
	}
	return m
}

// doneBlinkActive is true while any done-row flash is still running.
func (m Model) doneBlinkActive(now time.Time) bool {
	if len(m.BlinkUntil) == 0 || !blinkEnabled(m) {
		return false
	}
	for _, until := range m.BlinkUntil {
		if now.Before(until) {
			return true
		}
	}
	return false
}
