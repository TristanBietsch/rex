package tui

import (
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// orderedSessions returns the rows the board shows, in display order: Needs
// you → Working → Done. Filter is applied within each group, and a collapsed
// Done column contributes nothing, so j/k navigation matches the board.
func orderedSessions(m Model) []protocol.SessionSummary {
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for gi := range boardGroups {
		out = append(out, visibleRows(m, gi)...)
	}
	return out
}

// nextNeedsYou selects the next session waiting for input after the current
// selection, wrapping around (tab).
func nextNeedsYou(m Model) Model {
	rows := visibleRows(m, groupNeeds)
	if len(rows) == 0 {
		m.Notice = "nothing needs you"
		return m
	}
	next := 0
	for i, s := range rows {
		if s.ID == m.SelectedID {
			next = (i + 1) % len(rows)
			break
		}
	}
	m.SelectedID = rows[next].ID
	return ensureVisible(m)
}

// toggleDone handles 3: a long Done column folds and unfolds; a short one
// just gets the selection.
func toggleDone(m Model) Model {
	if len(filterByGroup(m.Sessions, boardGroups[groupDone], m.Filter)) <= doneCollapseAt {
		return jumpToSection(m, groupDone)
	}
	m.DoneExpanded = !m.DoneExpanded
	if m.DoneExpanded {
		return jumpToSection(m, groupDone)
	}
	return keepSelectionVisible(m)
}

// keepSelectionVisible moves the selection off a row the board no longer
// shows because the Done column folded (e.g. the row was just marked done),
// to the first row shown: the top of the board, which is what needs you
// first. Other selections are left alone. It also drops an expanded Done
// column back to folding once it is short again, so it folds the next time
// it grows past doneCollapseAt.
func keepSelectionVisible(m Model) Model {
	if m.DoneExpanded && len(filterByGroup(m.Sessions, boardGroups[groupDone], m.Filter)) <= doneCollapseAt {
		m.DoneExpanded = false
	}
	if m.SelectedID == "" || !doneCollapsed(m) || indexOfSelected(m) >= 0 {
		return m
	}
	hidden := false
	for _, s := range filterByGroup(m.Sessions, boardGroups[groupDone], m.Filter) {
		if s.ID == m.SelectedID {
			hidden = true
			break
		}
	}
	if !hidden {
		return m
	}
	m.SelectedID = ""
	if rows := orderedSessions(m); len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return ensureVisible(m)
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
	return stepSelection(m, delta, true)
}

// stepSelection moves the selection delta rows; sound plays the nav cue
// (the mouse wheel moves silently — a trackpad flick is dozens of steps).
func stepSelection(m Model, delta int, sound bool) Model {
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
		if sound && m.Audio != nil {
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
	if sound && m.SelectedID != rows[idx].ID && m.Audio != nil {
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
	n := 1
	if showFilterChips(m) {
		n++
	}
	if m.BackendUnavailable {
		n++
	}
	return n
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
// (0=Needs you, 1=Working, 2=Done). No-op if the group is empty.
func jumpToSection(m Model, groupIdx int) Model {
	if groupIdx < 0 || groupIdx >= len(boardGroups) {
		return m
	}
	rows := visibleRows(m, groupIdx)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return ensureVisible(m)
}

// filterTools is the filter chip order (header chips and the t cycle).
var filterTools = []string{"all", "claude", "codex", "gemini", "ollama"}

func cycleFilter(m Model) Model {
	next := 0
	for i, t := range filterTools {
		if t == m.Filter {
			next = (i + 1) % len(filterTools)
			break
		}
	}
	return setFilter(m, filterTools[next])
}

// setFilter switches the tool filter, keeping a still-visible selection.
func setFilter(m Model, filter string) Model {
	m.Filter = filter
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
