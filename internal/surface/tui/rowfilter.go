package tui

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// rowFilter is every way the board narrows what it shows. All board views
// (columns, navigation, counts) build one with rowFilterOf and pass it down,
// so a new filter is a field here and nothing else.
type rowFilter struct {
	tool   string          // "" / "all": any tool
	terms  []string        // search terms: every one must match (compileQuery)
	hidden map[string]bool // section IDs hidden outright
}

// rowFilterOf is the filter the model currently has active.
func rowFilterOf(m Model) rowFilter {
	var f rowFilter
	if m.featureOn("board_filter") {
		f.tool, f.hidden = m.Filter, m.Hidden
	}
	if m.featureOn("search") {
		f.terms = compileQuery(m.Search)
	}
	return f
}

// keep reports whether a session passes the row filters.
func (f rowFilter) keep(s protocol.SessionSummary) bool {
	if f.tool != "" && f.tool != "all" && s.ToolID != f.tool {
		return false
	}
	return sessionMatches(s, f.terms)
}

// showsSection reports whether the section is shown at all.
func (f rowFilter) showsSection(g boardGroup) bool {
	return !f.hidden[g.ID]
}

// searching reports whether a search narrows the rows.
func (f rowFilter) searching() bool {
	return len(f.terms) > 0
}

// rowsActive reports whether any filter narrows the rows.
func (f rowFilter) rowsActive() bool {
	return (f.tool != "" && f.tool != "all") || len(f.terms) > 0
}
