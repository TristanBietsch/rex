package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Window frame geometry. The terminal size (TermW x TermH) is kept apart from
// the layout size (Width x Height): every view lays out in Width x Height, and
// the frame, when shown, is drawn around it, so no view needs frame code.

// frameInset is how many cells the frame takes on each side: 0 when no frame
// is shown.
func frameInset(m Model) int {
	return 0
}

// relayout recomputes the layout size from the terminal size and the frame,
// then refits the attach popup and the board scroll. A Model with TermW == 0
// (tests, `rex render`) keeps the Width and Height it was built with.
func relayout(m Model) Model {
	if m.TermW > 0 {
		inset := frameInset(m)
		m.Width = m.TermW - 2*inset
		m.Height = m.TermH - 2*inset
	}
	resizeAttach(m)
	return ensureVisible(m)
}

// translateMouse maps a mouse event from screen to layout coordinates. It
// reports false for an event that is not on the layout area (on the frame).
func translateMouse(m Model, msg tea.MouseMsg) (tea.MouseMsg, bool) {
	inset := frameInset(m)
	if inset == 0 {
		return msg, true
	}
	ev := tea.MouseEvent(msg)
	x, y := ev.X-inset, ev.Y-inset
	if x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return msg, false
	}
	ev.X, ev.Y = x, y
	return tea.MouseMsg(ev), true
}
