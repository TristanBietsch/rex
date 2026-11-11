package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// helpGroup is one titled block of key/command rows in the help overlay.
type helpGroup struct {
	Title string
	Rows  [][2]string // {key, description}
}

const (
	helpKeyCap      = 22 // max key column width
	helpColGap      = 4
	helpTwoColMinW  = 96
	helpMinBoxW     = 60
	helpMaxBoxW     = 110
	helpTruncNote   = "(truncated, enlarge terminal)"
	helpGroupMouse  = "Mouse"
	helpGroupBadges = "Long-running badges"
)

// helpLeft and helpRight read the bindings and commands tables; only the
// badge legend is written out here. The section-number row is built from
// the sections the board has (sectionKeysRow).
func helpLeft(m Model) []helpGroup {
	nav := bindingsIn(groupNavigation)
	for i, r := range nav.Rows {
		if r[0] == sectionKeysLabel {
			nav.Rows[i] = sectionKeysRow(m)
		}
	}
	return []helpGroup{
		nav,
		bindingsIn(groupFilter),
		bindingsIn(groupActions),
		bindingsIn(groupMouse),
		bindingsIn(groupModes),
	}
}

// sectionKeysLabel is the Keys text of the section-number binding in the
// table; the help overlay swaps in sectionKeysRow.
const sectionKeysLabel = "1-4"

// sectionKeysRow is the help row for the number keys: one per active
// section, in screen order ("1-3" while Waiting is off).
func sectionKeysRow(m Model) [2]string {
	active := activeGroups(m)
	titles := make([]string, len(active))
	for n, gi := range active {
		titles[n] = boardGroups[gi].Title
	}
	return [2]string{
		"1-" + strconv.Itoa(len(active)),
		"jump to " + strings.Join(titles, " / ") + " (" + strconv.Itoa(len(active)) + " also folds a long Done column)",
	}
}

var helpRight = []helpGroup{
	commandsIn(groupSessionCmd),
	commandsIn(groupAppCmd),
	bindingsIn(groupAttached),
	{helpGroupBadges, [][2]string{
		{"▴12m", "dim"},
		{"▴45m", "amber"},
		{"▴3h", "red"},
		{"▴7h", "red inverse (time working)"},
	}},
}

// renderGroup renders one group at exactly-bounded width colW.
func renderGroup(g helpGroup, colW int) []string {
	kw := 0
	for _, r := range g.Rows {
		if w := ansi.StringWidth(r[0]); w > kw {
			kw = w
		}
	}
	if kw > helpKeyCap {
		kw = helpKeyCap
	}
	if max := colW / 2; kw > max {
		kw = max
	}
	dw := colW - kw - 2
	if dw < 8 {
		dw = 8
	}
	out := []string{sectionHeader(g.Title, colW)}
	for _, r := range g.Rows {
		key := ansi.Truncate(r[0], kw, "…")
		pad := strings.Repeat(" ", kw-ansi.StringWidth(key))
		descLines := strings.Split(ansi.Wrap(r[1], dw, ""), "\n")
		for i, d := range descLines {
			k := strings.Repeat(" ", kw)
			if i == 0 {
				k = styleSlug.Render(key) + pad
			}
			out = append(out, k+"  "+styleDim.Render(d))
		}
	}
	return out
}

func renderColumn(groups []helpGroup, colW int) string {
	var lines []string
	for i, g := range groups {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, renderGroup(g, colW)...)
	}
	return strings.Join(lines, "\n")
}

func filterGroups(gs []helpGroup, drop map[string]bool) []helpGroup {
	var out []helpGroup
	for _, g := range gs {
		if !drop[g.Title] {
			out = append(out, g)
		}
	}
	return out
}

// helpBox builds the bordered box at total width bw using cols (1 or 2)
// columns, omitting groups named in drop. note adds the truncation notice.
func helpBox(m Model, bw, cols int, drop map[string]bool, note bool) string {
	inner := bw - 8 // border 2 + padding 6
	left, right := filterGroups(helpLeft(m), drop), filterGroups(helpRight, drop)
	var body string
	if cols == 2 {
		colW := (inner - helpColGap) / 2
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			renderColumn(left, colW), strings.Repeat(" ", helpColGap), renderColumn(right, colW))
	} else {
		body = renderColumn(append(append([]helpGroup{}, left...), right...), inner)
	}
	footer := styleDim.Render("esc to close")
	if note {
		footer = styleDim.Render(helpTruncNote) + "\n" + footer
	}
	content := styleSlug.Render("HELP") + "\n\n" + body + "\n\n" + footer
	return panelStyle(bw).Render(content)
}

// renderHelp returns the finished bordered help box, fitting within w x h.
func renderHelp(m Model, w, h int) string {
	bw := w - 8
	if bw < helpMinBoxW {
		bw = helpMinBoxW
	}
	if bw > helpMaxBoxW {
		bw = helpMaxBoxW
	}
	if bw > w {
		bw = w
	}
	cols := 1
	if w >= helpTwoColMinW {
		cols = 2
	}
	drop := map[string]bool{}
	box := helpBox(m, bw, cols, drop, false)
	for _, g := range []string{helpGroupBadges, helpGroupMouse} {
		if lipgloss.Height(box) <= h-2 {
			break
		}
		drop[g] = true
		box = helpBox(m, bw, cols, drop, true)
	}
	if lipgloss.Height(box) > h {
		// Hard clip: keep the top h-1 lines plus the bottom border.
		lines := strings.Split(box, "\n")
		if h >= 2 {
			lines = append(lines[:h-1], lines[len(lines)-1])
		} else {
			lines = lines[:h]
		}
		box = strings.Join(lines, "\n")
	}
	return box
}
