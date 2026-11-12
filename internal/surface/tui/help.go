package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// helpRow is one key/command row of the help overlay.
type helpRow struct {
	Key, Desc string
	// Off marks a row whose feature switch is off: it is listed, dimmed
	// and tagged, so the overlay explains why the key does nothing.
	Off bool
}

// helpGroup is one titled block of rows in the help overlay.
type helpGroup struct {
	Title string
	Rows  []helpRow
	// Styled says the keys are already styled (the legend draws real
	// markers), so the layout must not restyle them.
	Styled bool
}

// HelpState is the help overlay's scroll position and `/` filter.
type HelpState struct {
	Scroll int
	// Filter keeps the rows whose key or description contains every word.
	Filter string
	// Filtering is whether keys edit Filter.
	Filtering bool
	// Group is the page's selected group (an index into helpGroups) and
	// Pane its focused pane (fullscreen_pages on).
	Group int
	Pane  pagePane
}

const (
	helpKeyCap      = 22 // max key column width
	helpColGap      = 4
	helpTwoColMinW  = 96
	helpMinBoxW     = 60
	helpMaxBoxW     = 110
	helpMaxFilter   = 64
	helpGroupLegend = "Legend"
)

// helpLeft and helpRight read the bindings and commands tables; the legend
// is drawn from the live styles (helpLegend). The section-number row is built
// from the sections the board has (sectionKeysRow).
func helpLeft(m Model) []helpGroup {
	nav := bindingsIn(groupNavigation)
	for i, r := range nav.Rows {
		if r.Key == sectionKeysLabel {
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
func sectionKeysRow(m Model) helpRow {
	active := activeGroups(m)
	titles := make([]string, len(active))
	for n, gi := range active {
		titles[n] = boardGroups[gi].Title
	}
	return helpRow{
		Key:  "1-" + strconv.Itoa(len(active)),
		Desc: "jump to " + strings.Join(titles, " / ") + " (" + strconv.Itoa(len(active)) + " also folds a long Done column)",
	}
}

var helpRight = []helpGroup{
	commandsIn(groupSessionCmd),
	commandsIn(groupAppCmd),
	bindingsIn(groupAttached),
}

// helpLegend is the marker legend, drawn with the styles the board uses, so
// the help can never describe a color the board does not show.
func helpLegend(m Model) helpGroup {
	frames := spinnerFramesFor(m)
	mark := func(st protocol.State, glyph string) string { return styleMarker[st].Render(glyph) }
	rows := []helpRow{
		{Key: mark(protocol.StateNeedsInput, stateGlyph(protocol.StateNeedsInput)), Desc: "ready"},
		{Key: mark(protocol.StateWorking, frames[m.SpinnerTick%len(frames)]), Desc: "working"},
	}
	if m.featureOn("waiting_section") {
		rows = append(rows, helpRow{Key: mark(protocol.StateWorking, waitingGlyph), Desc: "waiting on a tool, not on you"})
	}
	rows = append(rows,
		helpRow{Key: mark(protocol.StateQueued, stateGlyph(protocol.StateQueued)), Desc: "queued, agent not started"},
		helpRow{Key: mark(protocol.StateDone, stateGlyph(protocol.StateDone)), Desc: "done"},
		helpRow{Key: mark(protocol.StateFailed, stateGlyph(protocol.StateFailed)), Desc: "failed"},
		helpRow{Key: mark(protocol.StateCrashed, stateGlyph(protocol.StateCrashed)), Desc: "crashed"},
		helpRow{Key: styleFleet[0].Render("▍"), Desc: "fleet member (color per fleet)"},
		helpRow{Key: styleWorking.Render(subagentGlyph + " 3"), Desc: "subagents running"},
		helpRow{Key: styleLongRun[3].Render("▴ 10m"), Desc: "working 10 minutes or more"},
		helpRow{Key: styleLongRun[2].Render("▴ 30m"), Desc: "working 30 minutes or more"},
		helpRow{Key: styleLongRun[1].Render("▴ 2h"), Desc: "working 2 hours or more"},
		helpRow{Key: styleLongRun[0].Render("▴ 6h"), Desc: "working 6 hours or more"},
	)
	return helpGroup{Title: helpGroupLegend, Rows: rows, Styled: true}
}

// helpGroups is every help group in page order, rows of off features
// flagged: the left column's groups, the legend, then the right column's.
func helpGroups(m Model) []helpGroup {
	all := append(append(helpLeft(m), helpLegend(m)), helpRight...)
	return markOff(m, all)
}

// markOff flags the rows whose feature switch is off.
func markOff(m Model, groups []helpGroup) []helpGroup {
	out := make([]helpGroup, len(groups))
	for i, g := range groups {
		g.Rows = append([]helpRow(nil), g.Rows...)
		for j, r := range g.Rows {
			if flag := rowFlag(g.Title, r.Key); flag != "" && !m.featureOn(flag) {
				g.Rows[j].Off = true
			}
		}
		out[i] = g
	}
	return out
}

// rowFlag is the Features switch that gates the row with this group and key
// ("" = always on).
func rowFlag(group, key string) string {
	for _, b := range bindings {
		if b.Group == group && b.Keys == key {
			return b.Flag
		}
	}
	for _, c := range commands {
		if c.Group == group && c.helpKey() == key {
			return c.Flag
		}
	}
	return ""
}

// filterGroups keeps the rows whose plain key or description contains every
// term, and drops the groups left empty.
func filterGroups(groups []helpGroup, terms []string) []helpGroup {
	if len(terms) == 0 {
		return groups
	}
	var out []helpGroup
	for _, g := range groups {
		var rows []helpRow
		for _, r := range g.Rows {
			hay := strings.ToLower(ansi.Strip(r.Key) + " " + r.Desc)
			ok := true
			for _, t := range terms {
				if !strings.Contains(hay, t) {
					ok = false
					break
				}
			}
			if ok {
				rows = append(rows, r)
			}
		}
		if len(rows) > 0 {
			g.Rows = rows
			out = append(out, g)
		}
	}
	return out
}

// renderGroup renders one group at exactly-bounded width colW.
func renderGroup(g helpGroup, colW int) []string {
	colW = max(colW, 1)
	kw := 0
	for _, r := range g.Rows {
		if w := ansi.StringWidth(r.Key); w > kw {
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
		key := ansi.Truncate(r.Key, kw, "…")
		pad := strings.Repeat(" ", kw-ansi.StringWidth(key))
		desc, descStyle := r.Desc, styleDim
		keyStyle := styleKey
		switch {
		case g.Styled:
			keyStyle = lipgloss.NewStyle()
		case strings.HasPrefix(key, ":"):
			keyStyle = styleCmd
		}
		if r.Off {
			desc += " (off)"
			keyStyle, descStyle = styleMuted, styleMuted
		}
		descLines := strings.Split(ansi.Wrap(desc, dw, ""), "\n")
		for i, d := range descLines {
			k := strings.Repeat(" ", kw)
			if i == 0 {
				k = keyStyle.Render(key) + pad
			}
			out = append(out, k+"  "+descStyle.Render(d))
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

// helpBody lays every group (after the filter) out at box width bw in cols
// (1 or 2) columns: the scrollable text of the overlay.
func helpBody(m Model, bw, cols int) []string {
	filter := ""
	if m.Help != nil {
		filter = m.Help.Filter
	}
	left := filterGroups(markOff(m, append(helpLeft(m), helpLegend(m))), compileQuery(filter))
	right := filterGroups(markOff(m, helpRight), compileQuery(filter))
	if len(left)+len(right) == 0 {
		return []string{styleDim.Render("  no match for “" + protocol.FlattenDisplayText(filter) + "”")}
	}
	inner := max(bw-8, 1) // border 2 + padding 6
	if cols == 2 {
		colW := max((inner-helpColGap)/2, 1)
		return strings.Split(lipgloss.JoinHorizontal(lipgloss.Top,
			renderColumn(left, colW), strings.Repeat(" ", helpColGap), renderColumn(right, colW)), "\n")
	}
	return strings.Split(renderColumn(append(append([]helpGroup{}, left...), right...), inner), "\n")
}

// helpLayout is the finished overlay plus what the keys need to scroll it.
type helpLayout struct {
	box       []string
	maxScroll int // largest Scroll that still shows the last line
	page      int // rows a page key moves
}

// layoutHelp builds the bordered box, fitting within w x h: the body
// scrolls under ↑/↓ markers instead of dropping groups.
func layoutHelp(m Model, w, h int) helpLayout {
	bw := min(max(w-8, helpMinBoxW), helpMaxBoxW)
	if bw > w-2 {
		bw = max(w-2, 1)
	}
	cols := 1
	if w >= helpTwoColMinW {
		cols = 2
	}
	st := m.Help
	if st == nil {
		st = &HelpState{}
	}
	body := helpBody(m, bw, cols)

	var head []string
	switch {
	case st.Filtering:
		head = []string{styleArrow.Render("/ ") + stylePrimary.Render(protocol.FlattenDisplayText(st.Filter)) + cursorLit()}
	case st.Filter != "":
		head = []string{styleDim.Render("/ " + protocol.FlattenDisplayText(st.Filter))}
	}
	hints := [][2]string{{"j/k", "scroll"}, {"/", "filter"}, {"esc", "close"}}
	switch {
	case st.Filtering:
		hints = [][2]string{{"enter", "keep"}, {"esc", "clear"}}
	case st.Filter != "":
		hints = [][2]string{{"j/k", "scroll"}, {"/", "edit filter"}, {"esc", "clear"}}
	}
	footer := renderHints(hints, max(bw-8, 1))

	// Rows around the body: filter line, blank, footer, two border rows.
	vh := max(h-len(head)-1-1-2, 3)
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
	content := append(append(append([]string{}, head...), view...), "", footer)
	box, _ := panelLines("HELP", "rex "+version.Version, content, 3, 0, bw)
	if len(box) > h { // a terminal too short for the chrome: keep the bottom border
		box = append(box[:max(h-1, 1)], box[len(box)-1])
	}
	return helpLayout{box: box, maxScroll: maxScroll, page: max(vh-1, 1)}
}

// renderHelp returns the finished bordered help box, fitting within w x h.
func renderHelp(m Model, w, h int) string {
	return strings.Join(layoutHelp(m, w, h).box, "\n")
}

// renderHints is the footer format shared by the overlays: key caps in the
// key style, descriptions dim, " · " between, cut to width.
func renderHints(hints [][2]string, width int) string {
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleKey.Render(h[0]) + styleDim.Render(" "+h[1])
	}
	return ansi.Truncate(strings.Join(parts, styleMuted.Render(" · ")), width, "…")
}

// helpSize is the terminal size the help overlay is drawn for.
func helpSize(m Model) (w, h int) {
	w, h = m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	return w, h
}

// openHelp shows the help overlay from the top, unfiltered.
func openHelp(m Model) Model {
	m.Focus = FocusHelp
	m.Help = &HelpState{}
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m
}

func closeHelp(m Model) Model {
	m.Focus = FocusBoard
	m.Help = nil
	if m.Audio != nil {
		m.Audio.Play(audio.EventClose)
	}
	return m
}

// updateHelpKey handles keys while the help overlay is open.
func updateHelpKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := HelpState{}
	if m.Help != nil {
		st = *m.Help
	}
	w, h := helpSize(m)
	lay := helpLayoutFor(m, w, h)

	if st.Filtering {
		switch k.Type {
		case tea.KeyEsc:
			st.Filter, st.Filtering, st.Scroll = "", false, 0
		case tea.KeyEnter:
			st.Filtering = false
		case tea.KeyBackspace:
			if r := []rune(st.Filter); len(r) > 0 {
				st.Filter, st.Scroll = string(r[:len(r)-1]), 0
			}
		case tea.KeyCtrlU:
			st.Filter, st.Scroll = "", 0
		case tea.KeyRunes, tea.KeySpace:
			if add := strings.Map(func(r rune) rune {
				if r < ' ' || r == 0x7f {
					return -1
				}
				return r
			}, string(k.Runes)); len([]rune(st.Filter+add)) <= helpMaxFilter {
				if k.Type == tea.KeySpace {
					add = " "
				}
				st.Filter, st.Scroll = st.Filter+add, 0
			}
		default:
		}
		m.Help = &st
		return m, nil
	}

	if pagesOn(m) && helpPageKey(&st, k.String(), pageNarrow(w), len(helpGroups(m))) {
		m.Help = &st
		return m, nil
	}

	switch k.String() {
	case "esc":
		if st.Filter != "" {
			st.Filter, st.Scroll = "", 0
			break
		}
		return closeHelp(m), nil
	case "?":
		return closeHelp(m), nil
	case "q":
		if pagesOn(m) {
			return closeHelp(m), nil
		}
	case "/":
		st.Filtering = true
	case "j", "down":
		st.Scroll++
	case "k", "up":
		st.Scroll--
	case "pgdown", "ctrl+d", " ":
		st.Scroll += lay.page
	case "pgup", "ctrl+u":
		st.Scroll -= lay.page
	case "g", "home":
		st.Scroll = 0
	case "G", "end":
		st.Scroll = lay.maxScroll
	default:
	}
	st.Scroll = min(max(st.Scroll, 0), lay.maxScroll)
	m.Help = &st
	return m, nil
}

// helpPageKey handles the keys the page adds (fullscreen_pages on, not
// filtering) on st, for a page of n groups; false hands the key to the
// shared keys in updateHelpKey. A narrow page hides the group pane, so tab is a no-op there and a
// group-pane focus left over from a wider page acts as rows.
func helpPageKey(st *HelpState, key string, narrow bool, n int) bool {
	setGroup := func(g int) {
		if g != st.Group {
			st.Group, st.Scroll = g, 0
		}
	}
	switch key {
	case "tab", "shift+tab":
		if !narrow {
			if st.Pane == paneRows {
				st.Pane = paneSections
			} else {
				st.Pane = paneRows
			}
		}
		return true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if g := int(key[0] - '1'); g < n {
			setGroup(g)
		}
		return true
	case "[":
		setGroup((st.Group - 1 + n) % n)
		return true
	case "]":
		setGroup((st.Group + 1) % n)
		return true
	case "/":
		st.Filtering, st.Pane = true, paneRows
		return true
	case "esc", "q", "?":
		return false
	}
	if st.Pane == paneSections && !narrow {
		switch key {
		case "j", "down":
			setGroup(min(st.Group+1, n-1))
		case "k", "up":
			setGroup(max(st.Group-1, 0))
		case "enter", "l", "right":
			st.Pane = paneRows
		}
		return true
	}
	return false
}

// updateHelpMouse scrolls the help overlay with the wheel; on a page with
// the group pane focused, the wheel moves the group (clamped).
func updateHelpMouse(m Model, msg tea.MouseMsg) Model {
	if !tea.MouseEvent(msg).IsWheel() {
		return m
	}
	st := HelpState{}
	if m.Help != nil {
		st = *m.Help
	}
	w, h := helpSize(m)
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = +1
	default:
	}
	if pagesOn(m) && st.Pane == paneSections && !pageNarrow(w) {
		if g := min(max(st.Group+delta, 0), len(helpGroups(m))-1); g != st.Group {
			st.Group, st.Scroll = g, 0
		}
		m.Help = &st
		return m
	}
	maxScroll := helpLayoutFor(m, w, h).maxScroll
	st.Scroll += 3 * delta
	st.Scroll = min(max(st.Scroll, 0), maxScroll)
	m.Help = &st
	return m
}
