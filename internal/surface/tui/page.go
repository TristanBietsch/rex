package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

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
	pageChromeRows   = 5 // blank + header + divider + divider + hints
)

// pagesOn reports whether settings and help draw as full-screen pages.
func pagesOn(m Model) bool { return m.featureOn("fullscreen_pages") }

// pageNarrow reports whether a page w columns wide uses the single-column
// layout (no right header slot).
func pageNarrow(w int) bool { return w < pageNarrowW }

// pageHeader is the page's top: the board's blank row, the title row and a
// divider. The title row is the logo and title on the left, middle 4 cells
// after it and right flush right. middle and right are drawn as given
// (callers style them); right is dropped under pageNarrowW or when it does
// not fit, and middle is cut with … before the left part is touched.
func pageHeader(title, middle, right string, w int) []string {
	left := "  " + styleLogo.Render("∴ REX") + styleDim.Render(" › "+title)
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(right)
	if pageNarrow(w) || leftW+1+rightW > w {
		right, rightW = "", 0
	}
	used := leftW
	if rightW > 0 {
		used += 1 + rightW
	}
	row := left
	if middle != "" {
		if avail := w - used - 4; avail >= 2 {
			mid := ansi.Truncate(middle, avail, "…")
			row += "    " + mid
		}
	}
	if rightW > 0 {
		row += strings.Repeat(" ", max(1, w-ansi.StringWidth(row)-rightW)) + right
	}
	return []string{"", padLine(row, w), renderHR(w)}
}

// pageFooter is the page's bottom: a divider and the hint row, indented 2.
// show false (show_help_bar off, no modal prompt) keeps the row but blanks
// it, as the board does.
func pageFooter(hints [][2]string, w int, show bool) []string {
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

// settingsPageHints and settingsPageSearchHints are the settings page's
// footer; esc leads so a narrow page keeps it.
var (
	settingsPageHints = [][2]string{
		{"esc", "close"}, {"↑↓", "select"}, {"←→", "change"}, {"enter", "toggle"},
		{"/", "search"}, {"tab", "section"}, {"r", "reset"}, {"?", "help"},
	}
	settingsPageSearchHints = [][2]string{{"enter", "keep"}, {"esc", "clear"}, {"ctrl+u", "clear all"}}
	settingsPageEditHints   = [][2]string{{"enter", "save"}, {"esc", "cancel"}, {"ctrl+u", "clear"}}
)

// renderSettingsPage is the settings page for a w x h layout area.
func renderSettingsPage(m Model, w, h int) string {
	right := "config.yaml"
	if m.StorePath != "" {
		right = filepath.Base(m.StorePath)
	}
	right = styleDim.Render(right)
	if m.Settings == nil || m.Store == nil {
		return composePage(pageHeader("SETTINGS", "", right, w), nil,
			pageFooter(settingsPageHints, w, helpBarOn(m)), w, h)
	}
	st := m.Settings
	list := settingsList(st)

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

	inner := max(w-4, 1)
	bh := pageBodyHeight(h)
	listRows := max(bh-1-len(settingsDetailPane(m, list, inner)), 3)
	rows, detail := settingsBody(m, list, inner, listRows)
	body := indentLines(append(append(rows, ""), detail...), 2)

	hints, modal := settingsPageHints, false
	switch {
	case st.Edit != nil:
		hints, modal = settingsPageEditHints, true
	case st.Searching:
		hints, modal = settingsPageSearchHints, true
	}
	return composePage(pageHeader("SETTINGS", middle, right, w), body,
		pageFooter(hints, w, helpBarOn(m) || modal), w, h)
}

// helpPageHints is the help page's footer for its state; esc leads so a
// narrow page keeps it.
func helpPageHints(st *HelpState) [][2]string {
	switch {
	case st.Filtering:
		return [][2]string{{"enter", "keep"}, {"esc", "clear"}}
	case st.Filter != "":
		return [][2]string{{"esc", "clear"}, {"j/k", "scroll"}, {"/", "edit filter"}}
	}
	return [][2]string{{"esc", "close"}, {"j/k", "scroll"}, {"/", "filter"}}
}

// layoutHelpPage is the help page for a w x h layout area: box is the whole
// page, h lines; the body scrolls under ↑/↓ markers.
func layoutHelpPage(m Model, w, h int) helpLayout {
	st := m.Help
	if st == nil {
		st = &HelpState{}
	}
	cols := 1
	if w >= helpTwoColMinW {
		cols = 2
	}
	// helpBody lays out at bw-8 cells (an overlay's border and padding);
	// the page body is w-4 cells, indented 2.
	body := helpBody(m, w-4+8, cols)

	vh := pageBodyHeight(h)
	maxScroll := max(len(body)-vh, 0)
	start := min(max(st.Scroll, 0), maxScroll)
	view := body
	if len(body) > vh {
		view = append([]string(nil), body[start:start+vh]...)
		if start > 0 {
			view[0] = styleDim.Render("↑ more")
		}
		if start+vh < len(body) {
			view[len(view)-1] = styleDim.Render("↓ more")
		}
	}

	middle := ""
	if st.Filtering || st.Filter != "" {
		middle = pageQuery(st.Filter, st.Filtering)
	}
	page := composePage(
		pageHeader("HELP", middle, styleDim.Render("rex "+version.Version), w),
		indentLines(view, 2),
		pageFooter(helpPageHints(st), w, helpBarOn(m) || st.Filtering), w, h)
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
