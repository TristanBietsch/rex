package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// filterCommand runs `:filter`.
//
//	:filter                 clear every filter
//	:filter <tool>          show one tool's sessions ("all" shows every tool)
//	:filter hide <sec…>     hide sections
//	:filter show <sec…|all> show sections
//	:filter only <sec…>     show only these sections
//
// A section is an ID, a unique ID prefix, a title or a 1-based number
// (resolveSection).
func filterCommand(m Model, args []string) Model {
	if len(args) == 0 {
		m.Hidden = nil
		return setFilter(m, "all")
	}
	switch args[0] {
	case "hide", "show", "only":
		return filterSections(m, args[0], args[1:])
	}
	if len(args) != 1 || !knownFilterTool(m, args[0]) {
		m.Err = "filter: unknown tool or subcommand " + strings.Join(args, " ")
		return m
	}
	return setFilter(m, args[0])
}

// filterSections runs hide, show or only. It changes nothing when any
// reference is bad or the result would hide every section.
func filterSections(m Model, verb string, refs []string) Model {
	if len(refs) == 0 {
		m.Err = "filter: " + verb + " needs a section"
		return m
	}
	if verb == "show" && len(refs) == 1 && refs[0] == "all" {
		return showAllSections(m)
	}
	named := map[string]bool{}
	for _, ref := range refs {
		gi, err := resolveSection(ref)
		if err != nil {
			m.Err = "filter: " + err.Error()
			return m
		}
		named[boardGroups[gi].ID] = true
	}
	next := map[string]bool{}
	for _, g := range boardGroups {
		switch {
		case verb == "only":
			next[g.ID] = !named[g.ID]
		case named[g.ID]:
			next[g.ID] = verb == "hide"
		default:
			next[g.ID] = m.Hidden[g.ID]
		}
	}
	return applyHidden(m, next)
}

// applyHidden installs a hidden-section set (cloned from next, false
// entries dropped) and re-seats the view. It refuses a set that hides every
// section.
func applyHidden(m Model, next map[string]bool) Model {
	clean := map[string]bool{}
	for _, g := range boardGroups {
		if next[g.ID] {
			clean[g.ID] = true
		}
	}
	if len(clean) == len(boardGroups) {
		m.Err = "filter: can't hide every section"
		return m
	}
	if len(clean) == 0 {
		clean = nil
	}
	m.Hidden = clean
	return refilter(m)
}

// setSectionHidden hides or shows one section by ID. Hiding the last shown
// section is refused.
func setSectionHidden(m Model, id string, hide bool) Model {
	next := make(map[string]bool, len(m.Hidden)+1)
	for k, v := range m.Hidden {
		next[k] = v
	}
	next[id] = hide
	return applyHidden(m, next)
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

// chordSection is z...: zh toggles every completed section, z1-z9 toggle
// section N, z0 shows every section. Any other second key is not part of
// the chord.
func chordSection(m Model, second string) (Model, tea.Cmd, bool) {
	switch second {
	case "h":
		return toggleCompleted(m), nil, true
	case "0":
		return showAllSections(m), nil, true
	}
	n, err := strconv.Atoi(second)
	if err != nil || n < 1 || n > 9 {
		return m, nil, false
	}
	if n > len(boardGroups) {
		m.Notice = fmt.Sprintf("no section %d: use z1-z%d", n, len(boardGroups))
		return m, nil, true
	}
	id := boardGroups[n-1].ID
	return setSectionHidden(m, id, !m.Hidden[id]), nil, true
}

// toggleCompleted hides every completed section, or shows them when they
// are all hidden already.
func toggleCompleted(m Model) Model {
	hide := false
	for _, g := range boardGroups {
		if g.Completed && !m.Hidden[g.ID] {
			hide = true
		}
	}
	for _, g := range boardGroups {
		if g.Completed {
			m = setSectionHidden(m, g.ID, hide)
		}
	}
	return m
}

// showAllSections clears every hidden section.
func showAllSections(m Model) Model {
	m.Hidden = nil
	return refilter(m)
}

// renderFilterStatus is the dim note after the filter chips: which sections
// are hidden ("done hidden (zh)") and the active search ("/auth · 3 matches
// · esc clear"), or "" when neither applies. It sits after the chips so
// headerHitAt still finds every chip.
func renderFilterStatus(m Model) string {
	var notes []string
	if hidden := hiddenStatus(m); hidden != "" {
		notes = append(notes, hidden)
	}
	if search := searchStatus(m); search != "" {
		notes = append(notes, search)
	}
	if len(notes) == 0 {
		return ""
	}
	return styleDim.Render(strings.Join(notes, " · "))
}

// searchStatus is the search part of the filter status: the query, how many
// rows match, a hint to clear it (the editor has its own hints), and how
// many more matches sit in hidden sections.
func searchStatus(m Model) string {
	if !rowFilterOf(m).searching() {
		return ""
	}
	parts := []string{"/" + truncate(strings.TrimSpace(m.Search), 24), matchesLabel(searchMatchCount(m))}
	if m.Focus != FocusSearch {
		parts = append(parts, "esc clear")
	}
	if n := searchHiddenCount(m); n > 0 {
		parts = append(parts, fmt.Sprintf("+%d in hidden sections (z0)", n))
	}
	return strings.Join(parts, " · ")
}

// hiddenStatus names the hidden sections ("done hidden (zh)"), or "".
func hiddenStatus(m Model) string {
	var names []string
	var first int
	for i, g := range boardGroups {
		if m.Hidden[g.ID] {
			if len(names) == 0 {
				first = i
			}
			names = append(names, g.ID)
		}
	}
	if len(names) == 0 {
		return ""
	}
	hint := "z0 shows all"
	if len(names) == 1 {
		hint = "z" + strconv.Itoa(first+1)
		if boardGroups[first].Completed {
			hint = "zh"
		}
	}
	return strings.Join(names, ", ") + " hidden (" + hint + ")"
}

// hiddenSectionsFrom reads the board_hidden_sections setting.
func hiddenSectionsFrom(st *settings.Store) map[string]bool {
	if st == nil {
		return nil
	}
	raw, _ := st.Get("board_hidden_sections").(string)
	return hiddenSectionsFromIDs(strings.Split(raw, ","))
}

// hiddenSectionsFromIDs keeps the known section IDs (case-insensitive,
// trimmed). Unknown IDs are dropped, and a list that would hide every
// section is ignored. Nothing hidden is a nil map.
func hiddenSectionsFromIDs(ids []string) map[string]bool {
	known := map[string]bool{}
	for _, g := range boardGroups {
		known[g.ID] = true
	}
	out := map[string]bool{}
	for _, id := range ids {
		if id = strings.ToLower(strings.TrimSpace(id)); known[id] {
			out[id] = true
		}
	}
	if len(out) == 0 || len(out) == len(boardGroups) {
		return nil
	}
	return out
}
