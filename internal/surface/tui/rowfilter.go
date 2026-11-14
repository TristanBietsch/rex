package tui

import "github.com/tristanbietsch/rex/internal/wire/protocol"

// rowFilter is every way the board narrows what it shows. All board views
// (columns, navigation, counts) build one with rowFilterOf and pass it down,
// so a new filter is a field here and nothing else.
type rowFilter struct {
	// workspace is the fixed workspace id of a `rex here` board ("" = any).
	// It is not a user filter: no key or command changes it, and it does not
	// count as an active filter (rowsActive).
	workspace string
	tool      string          // "" / "all": any tool
	terms     []string        // search terms: every one must match (compileQuery)
	hidden    map[string]bool // section IDs hidden outright
	ctx       groupCtx        // clock and feature flag for grouping
}

// rowFilterOf is the filter the model currently has active.
func rowFilterOf(m Model) rowFilter {
	f := rowFilter{ctx: groupCtx{now: m.now(), waiting: m.featureOn("waiting_section")}, workspace: m.wsFilterID()}
	if m.featureOn("board_filter") {
		f.tool, f.hidden = m.Filter, m.Hidden
	}
	if m.featureOn("search") {
		f.terms = compileQuery(m.Search)
	}
	if len(f.hidden) > 0 {
		// Ids of sections the board does not have (Waiting, switched off)
		// are dropped: no chip, no status, and a stale list never blanks
		// the board.
		active := activeGroups(m)
		kept := map[string]bool{}
		for _, i := range active {
			if id := boardGroups[i].ID; f.hidden[id] {
				kept[id] = true
			}
		}
		f.hidden = kept
		if len(kept) == 0 || f.hidesAllOf(active) {
			f.hidden = nil
		}
	}
	return f
}

// keep reports whether a session passes the row filters.
func (f rowFilter) keep(s protocol.SessionSummary) bool {
	if f.workspace != "" && s.WorkspaceID != f.workspace {
		return false
	}
	if f.tool != "" && f.tool != "all" && s.ToolID != f.tool {
		return false
	}
	return sessionMatches(s, f.terms)
}

// showsSection reports whether the section is shown at all.
func (f rowFilter) showsSection(g boardGroup) bool {
	if g.ID == waitingGroupID && !f.ctx.waiting {
		return false // the feature is off: waiting sessions are in Working
	}
	return !f.hidden[g.ID]
}

// hidesAllOf reports whether every section in active (indexes into
// boardGroups) is hidden.
func (f rowFilter) hidesAllOf(active []int) bool {
	for _, i := range active {
		if !f.hidden[boardGroups[i].ID] {
			return false
		}
	}
	return true
}

// searching reports whether a search narrows the rows.
func (f rowFilter) searching() bool {
	return len(f.terms) > 0
}

// rowsActive reports whether any filter narrows the rows.
func (f rowFilter) rowsActive() bool {
	return (f.tool != "" && f.tool != "all") || len(f.terms) > 0
}
