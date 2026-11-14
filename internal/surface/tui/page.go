package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A full-screen page (settings, help) owns the whole layout area instead of
// floating over a dimmed board: the board's blank top row, a title row with
// the logo where the board has it, a divider, the body, a divider and a hint
// row. fullscreen_pages off draws the older overlays.

const (
	pageNarrowW      = 60
	pageSectionsMaxW = 24
	pageChromeRows   = 5           // blank + header + divider + divider + hints
	pageRightMargin  = meterMargin // the header's right slot and the rows end this far in
)

// pagePane is the focused pane of a page.
type pagePane int

const (
	paneRows     pagePane = iota // zero value: a page opens with rows focused
	paneSections                 // the section list on the left
)

// pagesOn reports whether settings and help draw as full-screen pages.
func pagesOn(m Model) bool { return m.featureOn("fullscreen_pages") }

// pageNarrow reports whether a page w columns wide uses the single-column
// layout (no right header slot).
func pageNarrow(w int) bool { return w < pageNarrowW }

// pageHeader is the page's top: the board's blank row, the title row and a
// divider. The title row is the logo and title on the left, middle 4 cells
// after it and right ending pageRightMargin cells in from the edge, as the
// board's usage meter does. middle and right are drawn as given
// (callers style them); right is dropped under pageNarrowW or when it does
// not fit, and middle is cut with … before the left part is touched.
func pageHeader(title, middle, right string, w int) []string {
	left := "  " + styleLogo.Render("∴ REX") + styleDim.Render(" › "+title)
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(right)
	if pageNarrow(w) || leftW+1+rightW+pageRightMargin > w {
		right, rightW = "", 0
	}
	used := leftW
	if rightW > 0 {
		used += 1 + rightW + pageRightMargin
	}
	row := left
	if middle != "" {
		if avail := w - used - 4; avail >= 2 {
			mid := ansi.Truncate(middle, avail, "…")
			row += "    " + mid
		}
	}
	if rightW > 0 {
		row += strings.Repeat(" ", max(1, w-pageRightMargin-ansi.StringWidth(row)-rightW)) + right
	}
	return []string{"", padLine(row, w), renderHR(w)}
}

// pageFooter is the page's bottom: a divider and the hint row, indented 2.
// A reconnect or error status takes the row first, as on the board (a
// locked setting's refusal shows here). show false (show_help_bar off, no
// modal prompt) keeps the row but blanks it, as the board does.
func pageFooter(m Model, hints [][2]string, w int, show bool) []string {
	if line, ok := statusLine(m, w); ok {
		return []string{renderHR(w), line}
	}
	row := ""
	if show {
		row = "  " + renderHints(hints, max(w-2, 1))
	}
	return []string{renderHR(w), row}
}

// pageBodyHeight is the number of body rows a page h rows tall has.
func pageBodyHeight(h int) int { return max(h-pageChromeRows, 1) }

// composePage stacks head, body and foot into exactly h lines, each padded
// or cut to w; the body is padded or cut to pageBodyHeight(h).
func composePage(head, body, foot []string, w, h int) string {
	bh := pageBodyHeight(h)
	if len(body) > bh {
		body = body[:bh]
	}
	lines := make([]string, 0, len(head)+bh+len(foot))
	lines = append(lines, head...)
	lines = append(lines, body...)
	for range bh - len(body) {
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = padLine(l, w)
	}
	return strings.Join(lines, "\n")
}

// pageQuery is the header text of a search or filter: the live query with
// a cursor while typing, else the kept query dimmed.
func pageQuery(q string, typing bool) string {
	q = protocol.FlattenDisplayText(q)
	if typing {
		return styleArrow.Render("/ ") + stylePrimary.Render(q) + cursorLit()
	}
	return styleDim.Render("/ " + q)
}

// indentLines prefixes every line with n spaces.
func indentLines(lines []string, n int) []string {
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = pad + l
	}
	return out
}

// paneItem is one row of a page's section pane: a title, a dim badge drawn
// flush right (a count, or ""), and Dim for a row with nothing to show (no
// search hits).
type paneItem struct {
	Title, Badge string
	Dim          bool
}

// sectionPaneWidth is the section pane's width, the page's 2-column indent
// and the ▸ marker included: the longest title and badge, capped at
// pageSectionsMaxW.
func sectionPaneWidth(items []paneItem) int {
	n := 0
	for _, it := range items {
		n = max(n, ansi.StringWidth(it.Title)+1+ansi.StringWidth(it.Badge))
	}
	return min(n+4, pageSectionsMaxW)
}

// renderSectionPane is the section pane, w wide (indent included) and at
// most h lines: a SECTIONS header, then the items windowed around cur. cur
// carries ▸; focused draws it on the selection, else its title in the
// accent. Badges are dim and flush right; Dim items are muted.
func renderSectionPane(items []paneItem, cur int, focused bool, w, h int) []string {
	inner := max(w-4, 1) // indent and marker
	sel := selectedStyle(stylePrimary)
	lines := make([]string, 0, len(items))
	for i, it := range items {
		badgeW := ansi.StringWidth(it.Badge)
		title := ansi.Truncate(it.Title, max(inner-badgeW-1, 1), "…")
		gap := strings.Repeat(" ", max(1, inner-ansi.StringWidth(title)-badgeW))
		if badgeW == 0 {
			gap = strings.Repeat(" ", max(0, inner-ansi.StringWidth(title)))
		}
		badge := ""
		if badgeW > 0 {
			badge = styleDim.Render(it.Badge)
		}
		switch {
		case i == cur && focused:
			if badgeW > 0 {
				badge = selectedStyle(styleDim).Render(it.Badge)
			}
			lines = append(lines, "  "+selectCursor()+sel.Bold(true).Render(title)+sel.Render(gap)+badge)
		case i == cur:
			lines = append(lines, "  "+selectCursor()+styleArrow.Render(title)+gap+badge)
		case it.Dim:
			lines = append(lines, "    "+styleMuted.Render(title)+gap+badge)
		default:
			lines = append(lines, "    "+stylePrimary.Render(title)+gap+badge)
		}
	}
	return append([]string{"  " + styleDim.Render("SECTIONS")}, windowLines(lines, cur, h-1)...)
}

// renderSectionStrip is a narrow page's one-line section pane: ‹ title ›
// centered in w.
func renderSectionStrip(title string, w int) string {
	strip := styleArrow.Render("‹ ") + styleBold.Render(title) + styleArrow.Render(" ›")
	return strings.Repeat(" ", max(0, (w-ansi.StringWidth(strip))/2)) + strip
}

// joinPanes sets right beside left (padded to leftW) with a │ between them,
// h lines.
func joinPanes(left, right []string, leftW, h int) []string {
	bar := styleBorderFg.Render(" │ ")
	out := make([]string, h)
	for i := range h {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = padLine(l, leftW) + bar + r
	}
	return out
}

// paneDivider is a w-wide divider under joinPanes(…, leftW, …) with ┴ under
// its │.
func paneDivider(leftW, w int) string {
	if w <= leftW+1 {
		return renderHR(w)
	}
	return styleBorderFg.Render(strings.Repeat("─", leftW+1) + "┴" + strings.Repeat("─", w-leftW-2))
}

// settingsPageHints is the settings page's footer for its state, focused
// pane and whether config.lua locks the cursor row; esc leads so a narrow
// page keeps it, and a narrow page (no section pane) says h/l step the
// section.
func settingsPageHints(st *SettingsState, pane pagePane, locked, narrow bool) [][2]string {
	hints := settingsPageHintsAll(st, pane, locked)
	if !narrow {
		return hints
	}
	for i, h := range hints {
		if h[0] == "h" {
			hints[i] = [2]string{"h/l", "section"}
		}
	}
	return slices.DeleteFunc(hints, func(h [2]string) bool { return h[0] == "[ ]" })
}

func settingsPageHintsAll(st *SettingsState, pane pagePane, locked bool) [][2]string {
	switch {
	case st.Edit != nil:
		out := make([][2]string, len(settingsEditHints))
		for i, h := range settingsEditHints {
			k, d, _ := strings.Cut(h, " ")
			out[i] = [2]string{k, d}
		}
		return out
	case st.Searching:
		return [][2]string{{"enter", "keep"}, {"esc", "cancel"}, {"ctrl+u", "clear"}}
	case pane == paneSections:
		return [][2]string{{"esc", "back"}, {"j/k", "section"}, {"l", "rows"}, {"gg/G", "first/last"}, {"/", "search"}}
	case locked:
		return [][2]string{
			{"esc", "back"}, {"j/k", "move"}, {"e", "open config.lua"}, {"/", "search"},
			{"h", "sections"}, {"[ ]", "section"}, {"?", "help"},
		}
	}
	return [][2]string{
		{"esc", "back"}, {"j/k", "move"}, {"←→", "change"}, {"enter", "toggle"}, {"/", "search"},
		{"h", "sections"}, {"[ ]", "section"}, {"r", "reset"}, {"?", "help"},
	}
}

// settingsPageRows is the rows pane at inner width, h lines: the section
// title then its rows, or under a search the hit count then the hits
// grouped by section (the no-match line when there are none).
func settingsPageRows(m Model, list []settings.Setting, inner, h int) []string {
	st := m.Settings
	labelW := settingsLabelW(inner)
	if st.Search == "" {
		lines, focus := settingsRowsAt(m, list, inner, labelW, false)
		sec := string(settingsSectionOf(st))
		return append([]string{styleBold.Render(sec)}, st.rowsView.window(sec, lines, focus, h-1, "  ")...)
	}
	lines, focus := settingsRowsAt(m, list, inner, labelW, true)
	if len(list) == 0 {
		return lines
	}
	count := fmt.Sprintf("%d matches", len(list))
	if len(list) == 1 {
		count = "1 match"
	}
	return append([]string{styleDim.Render(count)}, st.rowsView.window("/"+st.Search, lines, focus, h-1, "  ")...)
}

// settingsPageItems is the section pane's rows: each section with its
// modified count, dim under a search when it has no hits.
func settingsPageItems(m Model) []paneItem {
	st := m.Settings
	secs := settingsSections(st)
	items := make([]paneItem, len(secs))
	for i, sec := range secs {
		it := paneItem{Title: string(sec)}
		if n := settingsModifiedIn(m, sec); n > 0 {
			it.Badge = strconv.Itoa(n)
		}
		if st.Search != "" {
			_, hit := settingsFirstHit(st, sec)
			it.Dim = !hit
		}
		items[i] = it
	}
	return items
}

// renderSettingsPage is the settings page for a w x h layout area: the
// section pane and the rows side by side over the detail (the inspector),
// or under pageNarrowW a section strip over the rows and the detail.
func renderSettingsPage(m Model, w, h int) string {
	right := "config.yaml"
	if m.StorePath != "" {
		right = filepath.Base(m.StorePath)
	}
	right = styleDim.Render(right)
	if m.Settings == nil || m.Store == nil {
		return composePage(pageHeader("SETTINGS", "", right, w), nil,
			pageFooter(m, settingsPageHints(&SettingsState{}, paneRows, false, pageNarrow(w)), w, helpBarOn(m)), w, h)
	}
	st := m.Settings
	list := settingsPageList(m) // the cursor section's rows, or the search hits
	pane := st.Pane
	if pageNarrow(w) {
		pane = paneRows // a focus left over from a wider page
	}

	middle := ""
	switch {
	case st.Searching || st.Search != "":
		middle = pageQuery(st.Search, st.Searching)
	default:
		if i := settingsCursorIndex(list, st.CursorID); i > 0 {
			s := list[i-1]
			middle = styleDim.Render(string(s.Section) + " › " + s.Label)
		}
	}

	// The panes need a title and three rows (windowLines' least) for the
	// cursor row to stay on screen; on a short terminal the detail gives
	// way first, to its first two lines, then entirely.
	minPane := 4
	if pageNarrow(w) {
		minPane = 5 // the section strip too
	}
	detail := indentLines(settingsDetailPane(m, list, max(w-4, 1)), 2)
	for _, keep := range []int{len(detail), 2, 0} {
		if pageBodyHeight(h)-1-keep >= minPane || keep == 0 {
			detail = detail[:min(keep, len(detail))]
			break
		}
	}
	paneH := max(pageBodyHeight(h)-1-len(detail), minPane)
	var body []string
	if pageNarrow(w) {
		rowsH := max(paneH-1, 2)
		rows := settingsPageRows(m, list, max(w-2-pageRightMargin, 1), rowsH)
		body = append(body, renderSectionStrip(string(settingsSectionOf(st)), w))
		body = append(body, indentLines(rows, 2)...)
		for len(body) < paneH {
			body = append(body, "")
		}
		body = append(body, renderHR(w))
	} else {
		items := settingsPageItems(m)
		leftW := sectionPaneWidth(items)
		cur := slices.Index(settingsSections(st), settingsSectionOf(st))
		left := renderSectionPane(items, cur, pane == paneSections, leftW, paneH)
		rows := settingsPageRows(m, list, max(w-leftW-3-pageRightMargin, 1), paneH)
		body = append(joinPanes(left, rows, leftW, paneH), paneDivider(leftW, w))
	}
	body = append(body, detail...)

	modal := st.Edit != nil || st.Searching
	return composePage(pageHeader("SETTINGS", middle, right, w), body,
		pageFooter(m, settingsPageHints(st, pane, settingLockedAt(m, st.CursorID) != "", pageNarrow(w)), w, helpBarOn(m) || modal), w, h)
}

// helpPageHints is the help page's footer for its state, focused pane and
// width; esc leads so a narrow page keeps it, and a narrow page (no pane to
// tab to) leaves tab out.
func helpPageHints(st *HelpState, pane pagePane, narrow bool) [][2]string {
	switch {
	case st.Filtering:
		return [][2]string{{"enter", "keep"}, {"esc", "clear"}}
	case st.Filter != "":
		return [][2]string{{"esc", "clear"}, {"j/k", "scroll"}, {"/", "edit filter"}}
	case pane == paneSections:
		return [][2]string{{"esc", "back"}, {"j/k", "group"}, {"l", "rows"}, {"gg/G", "first/last"}, {"/", "filter"}}
	case narrow:
		return [][2]string{{"esc", "back"}, {"j/k", "scroll"}, {"h/l", "group"}, {"/", "filter"}}
	}
	return [][2]string{
		{"esc", "back"}, {"j/k", "scroll"}, {"ctrl+d/u", "half page"}, {"h", "groups"}, {"[ ]", "group"}, {"/", "filter"},
	}
}

// helpPageGroup is st's group, clamped to groups.
func helpPageGroup(st *HelpState, groups []helpGroup) int {
	return min(max(st.Group, 0), max(len(groups)-1, 0))
}

// helpPageRows is the help page's right pane at width colW: the selected
// group, or under a filter every matching row across the groups (the
// no-match line when there are none).
func helpPageRows(st *HelpState, groups []helpGroup, colW int) []string {
	if st.Filter == "" {
		return renderGroup(groups[helpPageGroup(st, groups)], colW)
	}
	hits := filterGroups(groups, compileQuery(st.Filter))
	if len(hits) == 0 {
		return []string{styleDim.Render("no match for “" + protocol.FlattenDisplayText(st.Filter) + "”")}
	}
	return strings.Split(renderColumn(hits, colW), "\n")
}

// helpPageItems is the group pane's rows: muted under a filter when the
// group has no hits.
func helpPageItems(st *HelpState, groups []helpGroup) []paneItem {
	terms := compileQuery(st.Filter)
	items := make([]paneItem, len(groups))
	for i, g := range groups {
		items[i] = paneItem{Title: g.Title}
		if len(terms) > 0 {
			items[i].Dim = len(filterGroups([]helpGroup{g}, terms)) == 0
		}
	}
	return items
}

// layoutHelpPage is the help page for a w x h layout area: box is the whole
// page, h lines. The group list sits left of the selected group's rows (or
// the filter's hits), or under pageNarrowW a group strip sits over them;
// the rows scroll under ↑/↓ markers, and maxScroll and page are theirs.
func layoutHelpPage(m Model, w, h int) helpLayout {
	st := m.Help
	if st == nil {
		st = &HelpState{}
	}
	groups := helpGroups(m)
	cur := helpPageGroup(st, groups)
	narrow := pageNarrow(w)
	pane := st.Pane
	if narrow {
		pane = paneRows // a focus left over from a wider page
	}

	bh := pageBodyHeight(h)
	var items []paneItem
	leftW, colW, vh := 0, max(w-2-pageRightMargin, 1), max(bh-1, 1)
	if !narrow {
		items = helpPageItems(st, groups)
		leftW = sectionPaneWidth(items)
		colW, vh = max(w-leftW-3-pageRightMargin, 1), bh
	}
	rows := helpPageRows(st, groups, colW)
	// The ↑ / ↓ markers take rows of their own (nav.go), so one scroll
	// step hides one row: the last first row leaves the ↑ marker and the
	// rest of the rows.
	maxScroll := 0
	if len(rows) > vh {
		maxScroll = len(rows) - (vh - 1)
	}
	start := min(max(st.Scroll, 0), maxScroll)
	view := rows
	if len(rows) > vh {
		from, to, up, down := navSpan(start, len(rows), vh)
		view = nil
		if up {
			view = append(view, styleDim.Render("↑ more"))
		}
		view = append(view, rows[from:to]...)
		if down {
			view = append(view, styleDim.Render("↓ more"))
		}
	}

	var body []string
	if narrow {
		body = append([]string{renderSectionStrip(groups[cur].Title, w)}, indentLines(view, 2)...)
	} else {
		left := renderSectionPane(items, cur, pane == paneSections, leftW, bh)
		body = joinPanes(left, view, leftW, bh)
	}

	middle := ""
	if st.Filtering || st.Filter != "" {
		middle = pageQuery(st.Filter, st.Filtering)
	}
	page := composePage(
		pageHeader("HELP", middle, styleDim.Render("rex "+version.Version), w), body,
		pageFooter(m, helpPageHints(st, pane, narrow), w, helpBarOn(m) || st.Filtering), w, h)
	return helpLayout{box: strings.Split(page, "\n"), maxScroll: maxScroll, page: max(vh-1, 1)}
}

// helpLayoutFor is the help layout the screen shows: the page or the
// overlay, by fullscreen_pages.
func helpLayoutFor(m Model, w, h int) helpLayout {
	if pagesOn(m) {
		return layoutHelpPage(m, w, h)
	}
	return layoutHelp(m, w, h)
}

// renderHelpPage is the help page for a w x h layout area.
func renderHelpPage(m Model, w, h int) string {
	return strings.Join(layoutHelpPage(m, w, h).box, "\n")
}
