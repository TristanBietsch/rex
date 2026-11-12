package tui

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// overviewOrder is the overlay's section order: what to look at first.
var overviewOrder = []overview.Bucket{overview.Review, overview.NeedsYou, overview.Failed, overview.Working, overview.Completed}

// overviewTitles are the section titles.
var overviewTitles = map[overview.Bucket]string{
	overview.Review:    "READY FOR REVIEW",
	overview.NeedsYou:  "NEEDS YOU",
	overview.Failed:    "FAILED",
	overview.Working:   "WORKING",
	overview.Completed: "COMPLETED (last 24 h)",
}

// overviewNameMax caps the name column.
const overviewNameMax = 24

// overviewRow is one selectable session in the overlay.
type overviewRow struct {
	bucket overview.Bucket
	sess   protocol.SessionSummary
}

// overviewRows lists the overlay's sessions in display order: the buckets of
// overview.Group, in overviewOrder, completed ones only from the last 24 h.
func overviewRows(m Model) []overviewRow {
	groups := overview.Group(m.Sessions)
	now := m.now()
	var rows []overviewRow
	for _, b := range overviewOrder {
		for _, s := range groups[b] {
			if !overview.Listed(b, s, now) {
				continue
			}
			rows = append(rows, overviewRow{bucket: b, sess: s})
		}
	}
	return rows
}

// overviewStatus is the row's text: the question a session waits on, else its
// description, else its task.
func overviewStatus(s protocol.SessionSummary) string {
	if s.State == protocol.StateNeedsInput && overview.IsQuestion(s.LastLine) {
		return strings.TrimSpace(protocol.FlattenDisplayText(s.LastLine))
	}
	return rowDescription(s)
}

// runOverview is the `:overview` command: it opens the overlay, or closes it
// when it is open. It works with overview_enabled off too: the lists are
// computed here, only the headline needs the daemon.
func runOverview(m Model, _ cmdCall) (Model, tea.Cmd) {
	if m.Focus == FocusOverview {
		m.Focus = FocusBoard
		return m, nil
	}
	m.Focus = FocusOverview
	m.OverviewSel = 0
	m.Err = ""
	slog.Debug("tui: overview overlay opened")
	return m, nil
}

// updateOverviewKey handles keys while the overlay is open: j/k move,
// enter attaches to the selected session, esc and q close.
func updateOverviewKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	rows := overviewRows(m)
	switch k.String() {
	case "esc", "q":
		m.Focus = FocusBoard
	case "j", "down":
		m.OverviewSel = min(m.OverviewSel+1, max(len(rows)-1, 0))
	case "k", "up":
		m.OverviewSel = max(m.OverviewSel-1, 0)
	case "g":
		m.OverviewSel = 0
	case "G":
		m.OverviewSel = max(len(rows)-1, 0)
	case "enter":
		if len(rows) == 0 {
			return m, nil
		}
		m.OverviewSel = min(m.OverviewSel, len(rows)-1)
		id := rows[m.OverviewSel].sess.ID
		m.Focus = FocusBoard
		m.SelectedID = id
		var cmd tea.Cmd
		m, cmd = chooseAttach(m, id)
		if m.Focus != FocusAttach { // the attach failed: stay on the overlay with the error
			m.Focus = FocusOverview
		}
		return m, cmd
	}
	return m, nil
}

// overviewInner is the overlay's text width on a w-column screen.
func overviewInner(w int) int { return max(min(w-12, 96), 36) }

// renderOverviewOverlay renders the overlay content (the border comes from
// centerOverlay): the daemon's headline, wrapped, then the sections. Every
// session string and the headline go through FlattenDisplayText; the lists
// are computed here from the sessions and never from the headline.
func renderOverviewOverlay(m Model) string {
	inner := overviewInner(m.Width)
	rows := overviewRows(m)
	sel := min(m.OverviewSel, max(len(rows)-1, 0))

	var head []string
	head = append(head, styleSectionTitle.Render("Overview"), "")
	if o := m.Overview; overviewOn(m) && o != nil && o.Enabled && o.Headline != "" {
		head = append(head, strings.Split(ansi.Wrap(protocol.FlattenDisplayText(o.Headline), inner, ""), "\n")...)
		if !o.GeneratedAt.IsZero() {
			head = append(head, styleMuted.Render("updated "+durationAgoAt(m.now(), o.GeneratedAt)+" ago"))
		}
		head = append(head, "")
	} else if !overviewOn(m) {
		head = append(head, styleDim.Render("Turn on overview_enabled in Settings for a one-line summary."), "")
	}

	nameW := 0
	for _, r := range rows {
		nameW = max(nameW, ansi.StringWidth(truncate(displayName(r.sess), overviewNameMax)))
	}

	var body []string
	lineOf := make([]int, len(rows)) // body line of each selectable row
	idx := 0
	for _, b := range overviewOrder {
		start := idx
		for idx < len(rows) && rows[idx].bucket == b {
			idx++
		}
		if idx == start {
			continue
		}
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, styleSectionTitle.Render(fmt.Sprintf("%s (%d)", overviewTitles[b], idx-start)))
		for i := start; i < idx; i++ {
			lineOf[i] = len(body)
			body = append(body, renderOverviewRow(m, rows[i], nameW, inner, i == sel))
		}
	}
	if len(rows) == 0 {
		body = append(body, styleDim.Render("Nothing running, waiting or finished right now."))
	}

	// Scroll the body so the selection stays in view on a short terminal.
	room := max(m.Height-len(head)-9, 5)
	if len(body) > room {
		selLine := 0
		if len(rows) > 0 {
			selLine = lineOf[sel]
		}
		start := min(max(selLine-room/2, 0), len(body)-room)
		body = body[start : start+room]
	}

	lines := slices.Concat(head, body)
	lines = append(lines, "", styleMuted.Render("  j/k select · enter attach · esc close"))
	return strings.Join(lines, "\n")
}

// renderOverviewRow is "marker name description age", cut to width.
func renderOverviewRow(m Model, r overviewRow, nameW, width int, selected bool) string {
	s := r.sess
	marker := stateMarkerCellFor(m, s.State, 0, false)
	name := truncate(displayName(s), overviewNameMax)
	name += strings.Repeat(" ", nameW-ansi.StringWidth(name))
	age := durationAgoAt(m.now(), overview.At(s))
	room := width - 2 - ansi.StringWidth(marker) - 1 - ansi.StringWidth(name) - 1 - 1 - len(age)
	text := ""
	if room > 3 {
		text = truncateWords(overviewStatus(s), room)
	}
	pointer := " "
	if selected {
		pointer = styleBold.Render("›")
	}
	line := pointer + " " + marker + " " + name + " " + styleDim.Render(text)
	pad := max(width-ansi.StringWidth(line)-len(age), 1)
	return line + strings.Repeat(" ", pad) + styleMuted.Render(age)
}
