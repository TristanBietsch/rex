package tui

import (
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Board mouse support. While mouse_enabled is on, rex captures the mouse from
// the board handoff onward: a click selects a row, a double-click opens it, the
// wheel moves the selection, and clicks on the filter chips / "+ new agent"
// act like t / n. The attach popup shares the same capture (attach_mouse.go).
// Capturing means the terminal no longer does native text selection; users
// hold Option (iTerm2/Terminal.app) or Shift (Ghostty/kitty) to bypass it.

// doubleClickWindow is the longest gap between two clicks on the same row
// that still counts as a double-click.
const doubleClickWindow = 350 * time.Millisecond

// wantMouse reports whether the terminal should be reporting mouse events:
// on everywhere after the boot splash (board, overlays, attach popup) unless
// mouse_enabled is off. Overlays drop the events they don't use instead of
// toggling capture per screen, which would flicker native selection on/off.
func wantMouse(m Model) bool {
	return m.Focus != FocusBoot && mouseEnabled(m)
}

// syncMouse turns terminal mouse reporting on or off to match wantMouse,
// emitting a command only on a transition. m.mouseOn tracks what rex last
// told the terminal, so the board, the attach popup and the settings page
// can all call this without double-enabling or releasing each other's
// capture.
func syncMouse(m Model, reason string) (Model, tea.Cmd) {
	want := wantMouse(m)
	if want == m.mouseOn {
		return m, nil
	}
	m.mouseOn = want
	slog.Info("tui: mouse capture", "on", want, "reason", reason)
	if want {
		return m, tea.EnableMouseCellMotion
	}
	return m, tea.DisableMouse
}

// updateMouse routes a mouse event to whatever owns the screen.
func updateMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.Attach != nil && (m.Focus == FocusAttach || m.Focus == FocusCommand) {
		return updateAttachMouse(m, msg)
	}
	if !mouseEnabled(m) {
		return m, nil
	}
	switch m.Focus {
	case FocusBoard:
		return updateBoardMouse(m, msg)
	case FocusSettings:
		return updateSettingsMouse(m, msg), nil
	}
	// Overlays (help, wizard, stats, fail) and bottom-bar prompts (command,
	// quick-spawn, rename, confirms) own input: drop the event so a click
	// can't change the board selection underneath them.
	if msg.Action == tea.MouseActionPress && !tea.MouseEvent(msg).IsWheel() {
		slog.Debug("tui: mouse press dropped (overlay focus)", "focus", m.Focus, "x", msg.X, "y", msg.Y)
	}
	return m, nil
}

// updateBoardMouse handles a mouse event on the bare board.
func updateBoardMouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		// The wheel moves the selection like j/k (and ensureVisible scrolls
		// the board with it). A viewport-only scroll would be snapped back
		// to the selection by the next daemon event.
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.PendingChord = ""
			return stepSelection(m, -1, false), nil
		case tea.MouseButtonWheelDown:
			m.PendingChord = ""
			return stepSelection(m, 1, false), nil
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	m.PendingChord = ""
	if id := sessionAtScreenRow(m, msg.Y); id != "" {
		return clickRow(m, id)
	}
	m.lastClickID = ""
	filter, newAgent := headerHitAt(m, msg.X, msg.Y)
	switch {
	case filter != "":
		slog.Debug("tui: mouse click on filter chip", "filter", filter)
		return setFilter(m, filter), nil
	case newAgent:
		slog.Debug("tui: mouse click on new agent")
		return openWizard(m)
	}
	return m, nil
}

// clickRow selects the clicked session; a second click on it within
// doubleClickWindow opens it (same path as enter).
func clickRow(m Model, id string) (Model, tea.Cmd) {
	now := m.now()
	double := m.lastClickID == id && now.Sub(m.lastClickAt) < doubleClickWindow
	m.SelectedID = id
	m = ensureVisible(m)
	if double {
		slog.Debug("tui: mouse double-click opens session", "session", id)
		m.lastClickID = ""
		return chooseAttach(m, id)
	}
	slog.Debug("tui: mouse click selects session", "session", id)
	m.lastClickID = id
	m.lastClickAt = now
	return m, nil
}

// updateSettingsMouse lets the wheel move the settings cursor; clicks are
// ignored (the rows are too dense to target reliably).
func updateSettingsMouse(m Model, msg tea.MouseMsg) Model {
	if m.Settings == nil {
		return m
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.Settings.CursorID = settingsAfter(m.Settings.CursorID, -1)
	case tea.MouseButtonWheelDown:
		m.Settings.CursorID = settingsAfter(m.Settings.CursorID, +1)
	}
	return m
}

// sessionAtScreenRow maps a screen row to the session rendered there, using
// the same slot layout and scroll offset as renderBoard. "" for non-rows.
func sessionAtScreenRow(m Model, y int) string {
	bh := boardHeight(m)
	rel := y - boardTop(m)
	if rel < 0 || rel >= bh {
		return ""
	}
	slots := boardSlots(m)
	line := clampScroll(m.ScrollOffset, len(slots), bh) + rel
	if line >= len(slots) || slots[line].kind != slotRow {
		return ""
	}
	return slots[line].sess.ID
}

// headerHitAt hit-tests the header (screen rows 1–2, see renderFullScreen):
// a filter chip returns its tool id, the "+ new agent" button newAgent. It
// searches the rendered header text, so it can't drift from the layout.
func headerHitAt(m Model, x, y int) (filter string, newAgent bool) {
	row := y - 1
	if row < 0 || row > 1 {
		return "", false
	}
	w := m.Width
	if w <= 0 {
		w = 100
	}
	lines := strings.Split(renderHeader(m, contentWidth(w)), "\n")
	if row >= len(lines) {
		return "", false
	}
	line := ansi.Strip(lines[row])
	if row == 0 {
		lo, hi, _, ok := cellSpan(line, "+ new agent", 0)
		return "", ok && x >= lo && x < hi
	}
	from := 0
	for _, t := range filterTools {
		lo, hi, next, ok := cellSpan(line, t, from)
		if !ok {
			return "", false
		}
		if x >= lo && x < hi {
			return t, false
		}
		from = next
	}
	return "", false
}

// cellSpan finds needle in line at or after byte offset from and returns its
// cell columns [lo, hi) plus the byte offset just past it.
func cellSpan(line, needle string, from int) (lo, hi, next int, ok bool) {
	if from > len(line) {
		return 0, 0, 0, false
	}
	i := strings.Index(line[from:], needle)
	if i < 0 {
		return 0, 0, 0, false
	}
	i += from
	lo = ansi.StringWidth(line[:i])
	return lo, lo + ansi.StringWidth(needle), i + len(needle), true
}
