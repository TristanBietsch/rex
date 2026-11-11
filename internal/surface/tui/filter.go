package tui

import "strconv"

// filterCommand runs `:filter`.
//
//	:filter <tool>   show one tool's sessions ("all" shows every tool)
func filterCommand(m Model, args []string) Model {
	if len(args) != 1 {
		return m
	}
	if !knownFilterTool(m, args[0]) {
		m.Err = "filter: unknown tool or subcommand " + args[0]
		return m
	}
	return setFilter(m, args[0])
}

// setSectionHidden hides or shows one section by ID. It clones Hidden, as
// Model is copied by value. Hiding the last shown section is refused.
func setSectionHidden(m Model, id string, hide bool) Model {
	if hide && !m.Hidden[id] {
		shown := 0
		for _, g := range boardGroups {
			if g.ID != id && !m.Hidden[g.ID] {
				shown++
			}
		}
		if shown == 0 {
			m.Err = "filter: can't hide every section"
			return m
		}
	}
	next := make(map[string]bool, len(m.Hidden)+1)
	for k, v := range m.Hidden {
		next[k] = v
	}
	if hide {
		next[id] = true
	} else {
		delete(next, id)
	}
	m.Hidden = next
	return refilter(m)
}

// hiddenNotice sets a Notice and reports true when section gi is hidden: a
// jump aimed at it would have nowhere to go.
func hiddenNotice(m *Model, gi int) bool {
	g := boardGroups[gi]
	if !m.Hidden[g.ID] {
		return false
	}
	hint := "z" + strconv.Itoa(gi+1)
	if g.Completed {
		hint = "zh"
	}
	m.Notice = g.Title + " is hidden — " + hint + " to show"
	return true
}

// adoptSnapshotFilter takes the daemon's snapshot filter only while the TUI
// has none of its own. The daemon always sends "all", so adopting it
// unconditionally reset the user's tool filter on every reconnect.
func adoptSnapshotFilter(m Model, snapFilter string) Model {
	if m.Filter == "" && snapFilter != "" {
		m.Filter = snapFilter
	}
	return m
}

// knownFilterTool reports whether tool is a filter chip or a tool with a
// session on the board.
func knownFilterTool(m Model, tool string) bool {
	for _, t := range filterTools {
		if t == tool {
			return true
		}
	}
	for _, s := range m.Sessions {
		if s.ToolID == tool {
			return true
		}
	}
	return false
}
