package tui

import (
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// orderedSessions returns sessions in display order: Needs input → Working → Completed.
// Filter is applied within each group. All terminal states (done/failed/crashed)
// are included so j/k navigation matches what the user sees on the board.
func orderedSessions(m Model) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for _, g := range boardGroups {
		out = append(out, filterByGroup(m.Sessions, g, m.Filter)...)
	}
	return out
}

func indexOfSelected(m Model) int {
	for i, s := range orderedSessions(m) {
		if s.ID == m.SelectedID {
			return i
		}
	}
	return -1
}

func moveSelection(m Model, delta int) Model {
	rows := orderedSessions(m)
	if len(rows) == 0 {
		m.SelectedID = ""
		return m
	}
	idx := indexOfSelected(m)
	if idx < 0 {
		if delta > 0 {
			m.SelectedID = rows[0].ID
		} else {
			m.SelectedID = rows[len(rows)-1].ID
		}
		if m.Audio != nil {
			m.Audio.Play("nav")
		}
		return ensureVisible(m)
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	if m.SelectedID != rows[idx].ID && m.Audio != nil {
		m.Audio.Play("nav")
	}
	m.SelectedID = rows[idx].ID
	return ensureVisible(m)
}

// selectedBoardLine returns the line index in the unscrolled board where the
// selected session is rendered, or -1 if nothing is selected or visible.
func selectedBoardLine(m Model) int {
	if m.SelectedID == "" {
		return -1
	}
	for i, sl := range boardSlots(m) {
		if sl.kind == slotRow && sl.sess.ID == m.SelectedID {
			return i
		}
	}
	return -1
}

// Screen layout around the board (see renderFullScreen):
//
//	1 blank · header (2 lines, +1 with the summarizer banner) · blank · HR · blank
//	board
//	HR · prompt · helpline
const (
	boardBottomRows = 3
	minBoardHeight  = 4
)

// headerHeight is the number of lines renderHeader produces.
func headerHeight(m Model) int {
	if m.BackendUnavailable {
		return 3
	}
	return 2
}

// boardTop is the screen row (0-based) of the first board line.
func boardTop(m Model) int {
	return 1 + headerHeight(m) + 3
}

// boardHeightFor is the board's visible line count on an h-row screen.
func boardHeightFor(m Model, h int) int {
	bh := h - boardTop(m) - boardBottomRows
	if bh < minBoardHeight {
		bh = minBoardHeight
	}
	return bh
}

// boardHeight is the board's visible line count for the current window.
func boardHeight(m Model) int {
	if m.Height <= 0 {
		return 20
	}
	return boardHeightFor(m, m.Height)
}

// ensureVisible adjusts m.ScrollOffset so the selected row is on-screen and
// the board never scrolls past its end.
func ensureVisible(m Model) Model {
	bh := boardHeight(m)
	if sel := selectedBoardLine(m); sel >= 0 {
		if sel < m.ScrollOffset {
			m.ScrollOffset = sel
		} else if sel >= m.ScrollOffset+bh {
			m.ScrollOffset = sel - bh + 1
		}
	}
	m.ScrollOffset = clampScroll(m.ScrollOffset, len(boardSlots(m)), bh)
	return m
}

// jumpToSection moves selection to the first row of the group at the given index
// (0=Needs input, 1=Working, 2=Completed). No-op if the group is empty.
func jumpToSection(m Model, groupIdx int) Model {
	if groupIdx < 0 || groupIdx >= len(boardGroups) {
		return m
	}
	rows := filterByGroup(m.Sessions, boardGroups[groupIdx], m.Filter)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return ensureVisible(m)
}

func cycleFilter(m Model) Model {
	tools := []string{"all", "claude", "codex", "gemini", "ollama"}
	next := 0
	for i, t := range tools {
		if t == m.Filter {
			next = (i + 1) % len(tools)
			break
		}
	}
	m.Filter = tools[next]
	// Filter change shifts visible rows — reset scroll and re-select to the top.
	m.ScrollOffset = 0
	rows := orderedSessions(m)
	if len(rows) > 0 {
		stillVisible := false
		for _, r := range rows {
			if r.ID == m.SelectedID {
				stillVisible = true
				break
			}
		}
		if !stillVisible {
			m.SelectedID = rows[0].ID
		}
	} else {
		m.SelectedID = ""
	}
	return ensureVisible(m)
}
