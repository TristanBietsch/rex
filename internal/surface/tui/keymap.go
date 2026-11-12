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
	if hiddenNotice(&m, groupNeeds) {
		return m
	}
	rows := visibleRows(m, groupNeeds)
	if len(rows) == 0 {
		m.Notice = "nothing ready"
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

// activeGroups is the indexes into boardGroups of the sections the board
// has, in screen order: all of them, except Waiting while the
// waiting_section feature is off. Number keys, z chords and section
// references count within it, so with the feature off 1/2/3 are Ready /
// Working / Done again.
func activeGroups(m Model) []int {
	on := m.featureOn("waiting_section")
	out := make([]int, 0, len(boardGroups))
	for i, g := range boardGroups {
		if g.ID == waitingGroupID && !on {
			continue
		}
		out = append(out, i)
	}
	return out
}

// sectionNumber is the 1-based number of boardGroups[gi] among the active
// sections (what its key and z chord say), or 0 when it is not active.
func sectionNumber(m Model, gi int) int {
	for n, i := range activeGroups(m) {
		if i == gi {
			return n + 1
		}
	}
	return 0
}

// jumpToNumber handles the number keys: n-th section on screen. The
// completed section's key also folds and unfolds a long column. A number
// past the last section does nothing.
func jumpToNumber(m Model, n int) Model {
	active := activeGroups(m)
	if n < 1 || n > len(active) {
		return m
	}
	if gi := active[n-1]; gi != groupDone {
		return jumpToSection(m, gi)
	}
	return toggleDone(m)
}

// toggleDone handles the Done key (4): a long Done column folds and unfolds; a short one
// just gets the selection.
func toggleDone(m Model) Model {
	if hiddenNotice(&m, groupDone) {
		return m
	}
	if doneRowCount(m) <= doneCollapseAt || rowFilterOf(m).searching() {
		return jumpToSection(m, groupDone)
	}
	m.DoneExpanded = !m.DoneExpanded
	if m.DoneExpanded {
		return jumpToSection(m, groupDone)
	}
	return keepSelectionVisible(m)
}

// keepSelectionVisible moves the selection off a live session the board no
// longer shows (the Done column folded, its section is hidden, or the tool
// filter excludes it: e.g. the row was just marked done), to the first row
// shown: the top of the board, which is what needs you
// first. Other selections are left alone. It also drops an expanded Done
// column back to folding once it is short again, so it folds the next time
// it grows past doneCollapseAt.
func keepSelectionVisible(m Model) Model {
	if m.DoneExpanded && doneRowCount(m) <= doneCollapseAt {
		m.DoneExpanded = false
	}
	// Cheap exit: nothing narrows the board, so the selection is visible.
	if m.SelectedID == "" || (len(rowFilterOf(m).hidden) == 0 && !doneCollapsed(m) && !rowFilterOf(m).rowsActive()) {
		return m
	}
	if _, live := findSession(m.Sessions, m.SelectedID); !live || indexOfSelected(m) >= 0 {
		return m // gone sessions are the remover's job (neighborOf)
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
//	1 blank · header (top row, +1 chip row, +1 overview line, +1 summarizer banner) · blank · HR · blank
//	board
//	HR · prompt · helpline
const (
	baseBottomRows = 3
	minBoardHeight = 4
)

// boardBottomRows is the rows under the board: the summary preview rows (when
// shown; see previewRows) plus HR, prompt and helpline.
func boardBottomRows(m Model) int { return previewRows(m) + baseBottomRows }

// headerHeight is the number of lines renderHeader produces.
func headerHeight(m Model) int {
	n := 1
	if showFilterChips(m) {
		n++
	}
	if showOverviewLine(m) {
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
	m.Height = h // the preview rows depend on the screen height being laid out
	bh := h - boardTop(m) - boardBottomRows(m)
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
		last := sel + rowLines(m) - 1 // a stacked row needs both lines on screen
		if sel < m.ScrollOffset {
			m.ScrollOffset = sel
		} else if last >= m.ScrollOffset+bh {
			m.ScrollOffset = last - bh + 1
		}
	}
	m.ScrollOffset = clampScroll(m.ScrollOffset, len(boardSlots(m)), bh)
	return m
}

// jumpToSection moves selection to the first row of the group at the given index
// (0=Ready, 1=Working, 2=Done). No-op if the group is empty.
func jumpToSection(m Model, groupIdx int) Model {
	if groupIdx < 0 || groupIdx >= len(boardGroups) {
		return m
	}
	if hiddenNotice(&m, groupIdx) {
		return m
	}
	rows := visibleRows(m, groupIdx)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return ensureVisible(m)
}

func cycleFilter(m Model) Model {
	tools := filterTools(m)
	next := 0
	for i, t := range tools {
		if t == m.Filter {
			next = (i + 1) % len(tools)
			break
		}
	}
	return setFilter(m, tools[next])
}

// setFilter switches the tool filter, keeping a still-visible selection.
func setFilter(m Model, tool string) Model {
	m.Filter = tool
	return refilter(m)
}

// refilter re-seats the view after any row filter changed: scroll resets and
// the selection stays if still visible, else moves to the first row.
func refilter(m Model) Model {
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
