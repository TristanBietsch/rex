package tui

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// rowFilter is every way the board narrows what it shows. All board views
// (columns, navigation, counts) build one with rowFilterOf and pass it down,
// so a new filter is a field here and nothing else.
type rowFilter struct {
	tool   string          // "" / "all": any tool
	terms  []string        // search terms (reserved)
	hidden map[string]bool // section IDs hidden outright (reserved)
}

// rowFilterOf is the filter the model currently has active.
func rowFilterOf(m Model) rowFilter {
	return rowFilter{tool: m.Filter}
}

// keep reports whether a session passes the row filters.
func (f rowFilter) keep(s protocol.SessionSummary) bool {
	return f.tool == "" || f.tool == "all" || s.ToolID == f.tool
}

// showsSection reports whether the section is shown at all.
func (f rowFilter) showsSection(g boardGroup) bool {
	return !f.hidden[g.ID]
}

// rowsActive reports whether any filter narrows the rows.
func (f rowFilter) rowsActive() bool {
	return (f.tool != "" && f.tool != "all") || len(f.terms) > 0
}
