package tui

import (
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// DaemonEventMsg wraps a single event from the daemon.
type DaemonEventMsg struct {
	Env protocol.Envelope
}

// DaemonErrMsg is sent when the connection to the daemon dies.
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
