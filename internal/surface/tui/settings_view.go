package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	// settingsHelpLines is how many help lines the detail pane shows;
	// settingsHelpLinesShort is the count on a terminal under
	// settingsTallHeight rows. The pane's height is fixed so the list never
	// jumps as the cursor moves.
	settingsHelpLines      = 4
	settingsHelpLinesShort = 2
	settingsTallHeight     = 30
	settingsHelpExpandMax  = 10
)

// settingModified reports whether the value differs from the default.
func settingModified(m Model, s settings.Setting) bool {
	return fmt.Sprint(m.Store.Get(s.ID)) != fmt.Sprint(s.Default)
}

// settingDefaultText is the default as the page shows a value.
func settingDefaultText(s settings.Setting) string {
	switch v := s.Default.(type) {
	case nil:
		return "—"
	case bool:
		if v {
			return "on"
		}
		return "off"
	case string:
		if v == "" {
			return "—"
		}
		return v
	default:
		return fmt.Sprint(v)
	}
}

// settingsDetail is the pane under the list for the selected setting: its
// id (what `rex config set` takes), default and when a change applies; the
// options with the current one lit; then help, cut to helpN lines unless
// expanded. It is exactly 2+helpN lines unless expanded.
func settingsDetail(m Model, s settings.Setting, inner, helpN int, expanded bool) []string {
	sep := styleMuted.Render(" · ")
	l1 := styleDim.Render("id ") + stylePrimary.Render(s.ID) + sep +
		styleDim.Render("default ") + stylePrimary.Render(settingDefaultText(s))
	if lbl := s.Apply.Label(); lbl != "" {
		l1 += sep + styleMuted.Render(lbl)
	}
	if s.ReadOnly {
		l1 += sep + styleMuted.Render("read-only")
	}
	if at := settingLockedAt(m, s.ID); at != "" {
		l1 += sep + styleMuted.Render("set in "+protocol.FlattenDisplayText(at))
	}
	if r, ok := onboardRowOf(m.Settings, s.ID); ok {
		// An Onboarding row is not a stored setting: no id or default.
		l1 = styleDim.Render("command ") + stylePrimary.Render(r.command()) + sep + styleMuted.Render("not on PATH")
	}
	if cur, ok := detectedModel(m, s.ID); ok && cur {
		l1 += sep + styleMuted.Render("detected")
	}
	out := []string{ansi.Truncate(l1, inner, "…")}

	// Options, or the range, or nothing.
	l2 := ""
	opts := s.Options
	if o, ok := dynamicOptions(m, s.ID); ok {
		opts = o
	}
	switch s.Type {
	case settings.TypeBool:
		opts = []string{"on", "off"}
	case settings.TypeInt, settings.TypeFloat:
		if s.Max > s.Min {
			l2 = styleDim.Render(fmt.Sprintf("range %g–%g", s.Min, s.Max))
		}
	default:
	}
	if len(opts) > 0 {
		cur := settingValueText(m, s)
		parts := make([]string, len(opts))
		for i, o := range opts {
			if o == cur {
				parts[i] = styleKey.Render(o)
			} else {
				parts[i] = styleDim.Render(o)
			}
		}
		l2 = strings.Join(parts, "  ")
	}
	out = append(out, ansi.Truncate(l2, inner, "…"))

	// Help.
	var wrapped []string
	if s.Help != "" {
		wrapped = strings.Split(ansi.Wordwrap(s.Help, inner, ""), "\n")
	}
	n := helpN
	if expanded {
		n = min(len(wrapped), settingsHelpExpandMax)
	}
	for i := range n {
		line := ""
		if i < len(wrapped) {
			line = wrapped[i]
		}
		if !expanded && i == n-1 && len(wrapped) > n {
			line = ansi.Truncate(line, max(inner-8, 1), "…") + styleMuted.Render(" ? more")
		}
		out = append(out, styleDim.Render(ansi.Truncate(line, inner, "…")))
	}
	if e := m.Settings.Edit; e != nil && e.ID == s.ID {
		// The editor takes the option strip's line; the reason a save was
		// refused takes the first help line.
		draft := stylePrimary.Render(protocol.FlattenDisplayText(e.Text()))
		if w := inner - 3; ansi.StringWidth(draft) > w {
			draft = ansi.TruncateLeft(draft, ansi.StringWidth(draft)-w+1, "…")
		}
		out[1] = styleArrow.Render("› ") + draft + cursorLit()
		if e.Err != "" {
			out[2] = styleFailed.Render(ansi.Truncate(protocol.FlattenDisplayText(e.Err), inner, "…"))
		} else {
			out[2] = renderHints([][2]string{{"enter", "save"}, {"esc", "cancel"}, {"ctrl+u", "clear"}}, inner)
		}
	}
	return out
}

// settingOnStyle and settingOffStyle draw a switch's value: on in the
// primary tone, off in the dim one, so on stands out in every theme. sel is
// the selected row, drawn on the selection.
func settingOnStyle(sel bool) lipgloss.Style {
	if sel {
		return selectedStyle(stylePrimary)
	}
	return stylePrimary
}

func settingOffStyle(sel bool) lipgloss.Style {
	if sel {
		return selectedStyle(styleDim)
	}
	return styleDim
}

// settingValueStyle is the style of a row's value: off, a read-only or an
// unset value dim; on and any other value primary.
func settingValueStyle(s settings.Setting, val string, empty, sel bool) lipgloss.Style {
	if s.ReadOnly || empty || (s.Type == settings.TypeBool && val == "off") {
		return settingOffStyle(sel)
	}
	return settingOnStyle(sel)
}

// settingsLabelW is the label column's width for a list inner cells wide.
func settingsLabelW(inner int) int {
	return min(30, inner*40/100)
}

// settingsCursorIndex is the 1-based position of the cursor row in list (0
// when it is not there).
func settingsCursorIndex(list []settings.Setting, id string) int {
	idx := 0
	for i, s := range list {
		if s.ID == id {
			idx = i + 1
		}
	}
	return idx
}

// settingsDetailPane is the detail pane for the cursor row of list, padded
// to its fixed height (2 + the help lines) so the list never jumps.
func settingsDetailPane(m Model, list []settings.Setting, inner int) []string {
	helpN := settingsHelpLines
	if m.Height > 0 && m.Height < settingsTallHeight {
		helpN = settingsHelpLinesShort
	}
	var detail []string
	if i := settingsCursorIndex(list, m.Settings.CursorID); i > 0 {
		detail = settingsDetail(m, list[i-1], inner, helpN, m.Settings.HelpExpanded)
	}
	for len(detail) < 2+helpN {
		detail = append(detail, "")
	}
	return detail
}

// settingsBodyAt is the overlay's list and detail at inner width, the
// label column labelW wide: rows is the list (section headers and setting
// rows) windowed to listRows, detail the pane under it; the caller puts the
// blank row between them.
func settingsBodyAt(m Model, list []settings.Setting, inner, labelW, listRows int) (rows []string, detail []string) {
	lines, focus := settingsRowsAt(m, list, inner, labelW, true)
	return windowLines(lines, focus, listRows), settingsDetailPane(m, list, inner)
}

// settingsRowsAt is the unwindowed rows of list at inner width with the
// label column labelW wide, and the index of the cursor's line. headers puts
// a section header before each section's rows; an empty list is the
// no-match line.
func settingsRowsAt(m Model, list []settings.Setting, inner, labelW int, headers bool) (lines []string, focus int) {
	st := m.Settings
	ownLine := inner < 40
	selStyle := selectedStyle(stylePrimary)
	selBold := selStyle.Bold(true)

	curSection := ""
	for _, s := range list {
		if headers && string(s.Section) != curSection {
			if curSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, sectionHeader(string(s.Section), inner))
			curSection = string(s.Section)
		}
		isSel := s.ID == st.CursorID
		adjustable := settingAdjustable(m, s)
		val := settingValueText(m, s)
		empty := val == "—"
		label := ansi.Truncate(s.Label, labelW, "…")

		// The two cells before a value hold the modified bullet.
		valAvail := inner - 2 - labelW - 1 - 2
		if ownLine {
			valAvail = inner - 2 - 2 // indent, bullet: flush right like the rules
		}
		arrows := isSel && adjustable && !empty
		if arrows {
			valAvail -= 4
		}
		badge := settingLockBadge(m, s)
		locked := badge != ""
		if locked {
			valAvail -= 4 // " lua"
			badge = " " + badge
		}
		if valAvail < 1 {
			valAvail = 1
		}
		val = ansi.Truncate(val, valAvail, "…")
		valCell := val
		if arrows {
			valCell = "◂ " + val + " ▸"
		}
		bullet := "  "
		if settingModified(m, s) {
			bullet = "• "
		}

		// Values sit flush right, the modified bullet just before them: gap
		// fills the cells the value leaves.
		gap := strings.Repeat(" ", max(0, valAvail-ansi.StringWidth(val)))
		vs := settingValueStyle(s, val, empty, isSel)
		if locked { // config.lua sets it: drawn dim, like an off value
			vs = settingOffStyle(isSel)
		}

		if !isSel {
			b := styleDim.Render(bullet)
			if bullet != "  " {
				b = styleArrow.Render(bullet)
			}
			if ownLine {
				lines = append(lines, "  "+styleDim.Render(label), "  "+gap+b+vs.Render(valCell)+badge)
			} else {
				lines = append(lines, "  "+styleDim.Render(padCells(label, labelW))+" "+gap+b+vs.Render(valCell)+badge)
			}
			continue
		}

		if locked { // keep the selection background under the badge
			badge = selStyle.Render(" ") + selStyle.Foreground(colorFgMuted).Render("lua")
		}
		focus = len(lines)
		marker := styleArrow.Render("▸ ")
		b := selStyle.Render(bullet)
		if bullet != "  " {
			b = selStyle.Foreground(colorAccent).Render(bullet)
		}
		if ownLine {
			first := marker + selBold.Render(label)
			lines = append(lines, first+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(first)))))
			second := selStyle.Render("  "+gap) + b + vs.Render(valCell) + badge
			lines = append(lines, second+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(second)))))
			continue
		}
		row := marker + selBold.Render(padCells(label, labelW)) + selStyle.Render(" "+gap) + b + vs.Render(valCell) + badge
		lines = append(lines, row+selStyle.Render(strings.Repeat(" ", max(0, inner-ansi.StringWidth(row)))))
	}
	if len(list) == 0 {
		lines = append(lines, styleDim.Render("  no setting matches “"+protocol.FlattenDisplayText(st.Search)+"”"))
	}
	return lines, focus
}

// renderSettings returns the finished settings box for a termW-wide terminal.
func renderSettings(m Model, termW int) string {
	if m.Settings == nil || m.Store == nil {
		return ""
	}
	st := m.Settings
	if termW <= 0 {
		termW = 100
	}

	var (
		width, padH int
		bordered    = termW >= 40
	)
	switch {
	case termW < 40:
		width, padH = termW, 0
	case termW < 60:
		width, padH = termW-2, 1
	default:
		width, padH = termW-8, 3
		if width < 56 {
			width = 56
		}
		if width > 88 {
			width = 88
		}
	}
	inner := width
	if bordered {
		inner = width - 2 - 2*padH
	}
	labelW := settingsLabelW(inner)
	if termW < 60 {
		labelW = inner / 2
	}

	list := settingsList(st)
	cursorIdx := settingsCursorIndex(list, st.CursorID)

	// Header: SETTINGS left, "n of N" right. A bordered panel carries both
	// in its top edge; the borderless narrow layout keeps a header row.
	posText := fmt.Sprintf("%d of %d", cursorIdx, len(list))
	var header string
	if !bordered {
		title := styleBold.Render("SETTINGS")
		pos := styleDim.Render(posText)
		gap := inner - ansi.StringWidth(title) - ansi.StringWidth(pos)
		header = title + strings.Repeat(" ", max(1, gap)) + pos
		if gap < 1 {
			header = ansi.Truncate(title, inner, "")
		}
	}

	var searchLine []string
	switch {
	case st.Searching:
		searchLine = []string{styleArrow.Render("/ ") + stylePrimary.Render(protocol.FlattenDisplayText(st.Search)) + cursorLit()}
	case st.Search != "":
		searchLine = []string{styleDim.Render("/ " + protocol.FlattenDisplayText(st.Search))}
	}

	hints := settingsHints
	if cursorIdx > 0 && settingLockedAt(m, list[cursorIdx-1].ID) != "" {
		hints = settingsLockedHints
	}
	if st.Edit != nil {
		hints = settingsEditHints
	}
	footer := wrapHints(hints, inner)

	// Non-body rows: search line, blank, detail, blank, footer, plus two
	// more: the borderless layout's header and blank, or a bordered panel's
	// two border rows (its title sits in the top one). The detail pane has
	// a fixed height so the list never jumps.
	chrome := len(searchLine) + 1 + len(settingsDetailPane(m, list, inner)) + 1 + len(footer) + 2
	maxRows := 0
	if m.Height > 0 {
		maxRows = max(m.Height-chrome, 3)
	}
	rows, detail := settingsBodyAt(m, list, inner, labelW, maxRows)

	var out []string
	if !bordered {
		out = append(out, header, "")
	}
	out = append(out, searchLine...)
	out = append(out, rows...)
	out = append(out, "")
	out = append(out, detail...)
	out = append(out, "")
	out = append(out, footer...)
	content := strings.Join(out, "\n")

	if !bordered {
		return content
	}
	box, _ := panelLines("SETTINGS", posText, strings.Split(content, "\n"), padH, 0, width)
	return strings.Join(box, "\n")
}
