package tui

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
