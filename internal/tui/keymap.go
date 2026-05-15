package tui

import (
	"strings"

	"github.com/tristanbietsch/rex/internal/protocol"
)

// orderedSessions returns sessions in display order: Needs input → Working → Completed → others.
// Filter is applied within each group.
func orderedSessions(m Model) []protocol.SessionSummary {
	groups := []protocol.State{protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone}
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for _, st := range groups {
		out = append(out, filterByState(m.Sessions, st, m.Filter)...)
	}
	// Then any other (failed, crashed) at the bottom.
	for _, s := range m.Sessions {
		switch s.State {
		case protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone:
			continue
		}
		if m.Filter != "all" && m.Filter != "" && s.ToolID != m.Filter {
			continue
		}
		out = append(out, s)
	}
	// Apply search-query filter (set by /find).
	if q := strings.ToLower(strings.TrimSpace(m.SearchQuery)); q != "" {
		filtered := out[:0]
		for _, s := range out {
			if strings.Contains(strings.ToLower(s.Slug), q) ||
				strings.Contains(strings.ToLower(s.LastLine), q) ||
				strings.Contains(strings.ToLower(s.Title), q) {
				filtered = append(filtered, s)
			}
		}
		out = filtered
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
		// Nothing selected — start at top or bottom based on delta sign.
		if delta > 0 {
			m.SelectedID = rows[0].ID
		} else {
			m.SelectedID = rows[len(rows)-1].ID
		}
		if m.Audio != nil {
			m.Audio.Play("nav")
		}
		return m
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
	return m
}

func jumpToSection(m Model, st protocol.State) Model {
	rows := filterByState(m.Sessions, st, m.Filter)
	if len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return m
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
	return m
}
