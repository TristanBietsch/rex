package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// featureSummaryPreview is the Features switch for the preview line.
const featureSummaryPreview = "summary_preview"

const (
	previewMinHeight  = 20 // below this the line is not worth a board row
	previewTallHeight = 30 // from here a long detail level gets two lines
)

// previewIndent prefixes the first preview line; continuation lines align
// under the text.
const (
	previewLead   = "  ↳ "
	previewIndent = "    "
)

// previewRows is how many rows sit between the board and the bottom rule for
// the selected session's lead: 0 when the feature is off, the terminal is
// short, in the mobile layout, or no session has anything to show yet; 2 on
// a tall terminal at the long detail level; else 1. "Anything to show" is a
// description with row_icons off, and a lead (previewLeadText) with it on,
// since then the line is the only place a row's words show. Whether the
// selected row has anything to show does not matter: the rows are reserved
// for the whole board, so the board height does not jump as the selection
// moves.
func previewRows(m Model) int {
	if !settings.FeatureOn(m.Store, featureSummaryPreview) || m.Height < previewMinHeight ||
		mobileLayout(m) { // a stacked row's second line already shows the text
		return 0
	}
	if rowIconsOn(m) {
		if !anyLead(m) {
			return 0
		}
	} else if !anyDescription(m) {
		return 0
	}
	if m.Height >= previewTallHeight && m.Store != nil && m.Store.String("summary_detail") == "long" {
		return 2
	}
	return 1
}

func anyDescription(m Model) bool {
	for _, s := range m.Sessions {
		if s.Description != "" {
			return true
		}
	}
	return false
}

func anyLead(m Model) bool {
	for _, s := range m.Sessions {
		if t, _ := previewLeadText(s); t != "" {
			return true
		}
	}
	return false
}

// previewLeadText is what the line shows for s with row_icons on, first that
// exists: the question it waits on (rowQuestion), its description, or
// "task: " + its title; "" when none. isSummary says the lead is the
// description, the only lead that carries an age. Same order as the attach
// banner's lead. Flattened: all three are agent or user supplied.
func previewLeadText(s protocol.SessionSummary) (text string, isSummary bool) {
	if q := rowQuestion(s); q != "" {
		return q, false
	}
	if d := strings.TrimSpace(protocol.FlattenDisplayText(s.Description)); d != "" {
		return d, true
	}
	if t := strings.TrimSpace(protocol.FlattenDisplayText(s.Title)); t != "" {
		return "task: " + t, false
	}
	return "", false
}

// previewSession is the session the preview shows, or false when there is
// nothing to show. With row_icons on: no selection or the selected row has
// no lead. With it off: no selection, no description, or (in the detailed
// view, where the row already carries the description) the row shows all of
// it.
func previewSession(m Model, cw int) (protocol.SessionSummary, bool) {
	for _, s := range m.Sessions {
		if s.ID != m.SelectedID {
			continue
		}
		if rowIconsOn(m) {
			t, _ := previewLeadText(s)
			return s, t != ""
		}
		if s.Description == "" {
			return s, false
		}
		if m.Detailed && ansi.StringWidth(rowDescription(s)) <= boardLayout(m, cw).desc {
			return s, false
		}
		return s, true
	}
	return protocol.SessionSummary{}, false
}

// renderDescPreview renders exactly previewRows(m) lines, each cw wide: the
// selected session's full summary (with row_icons on, its lead) and, for a
// summary, its age, wrapped, the last line cut with an ellipsis. Blank lines
// when there is nothing to show.
func renderDescPreview(m Model, cw int) []string {
	n := previewRows(m)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = padLine("", cw)
	}
	s, ok := previewSession(m, cw)
	if !ok || n == 0 {
		return lines
	}
	desc, isSummary := protocol.FlattenDisplayText(s.Description), true
	if rowIconsOn(m) {
		desc, isSummary = previewLeadText(s)
	}
	age := ""
	if isSummary && !s.DescriptionAt.IsZero() {
		age = " · " + durationAgoAt(m.now(), s.DescriptionAt)
	}
	width := max(cw-len(previewIndent), 1)
	wrapped := strings.Split(ansi.Wrap(desc+age, width, ""), "\n")
	if len(wrapped) > n {
		// Too long: keep the first lines, cut the rest short of the age, and
		// end the last line with the age so it always shows.
		wrapped = strings.Split(ansi.Wrap(desc, width, ""), "\n")
		if len(wrapped) > n {
			wrapped = append(wrapped[:n-1], strings.Join(wrapped[n-1:], " "))
		}
		ageW := ansi.StringWidth(age)
		if width-ageW < 8 {
			ageW = 0 // no room for both; the text wins
			age = ""
		}
		last := truncate(wrapped[len(wrapped)-1], width-ageW)
		wrapped[len(wrapped)-1] = last + age
	}
	dim := styleDim
	for i, w := range wrapped {
		prefix := previewIndent
		if i == 0 {
			prefix = previewLead
		}
		lines[i] = padLine(dim.Render(prefix+w), cw)
	}
	return lines
}
