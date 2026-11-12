package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Mobile rows. A session takes two lines, each exactly width wide:
//
//	▸ ⚑  release-notes            ⋔ 2  ▴ 42m  11m
//	     Which feature should lead: search or export…
//
// Line 1 is the indent, state marker, name (the flexible cell), the subagent
// and long-run cells and the age. With row_icons on, the unseen mark and the
// 1-cell why glyph (?) follow the name: "dark-mode  ✦ ?". Line 2 is the
// text under the name.
const (
	mobileTextIndent = rowIndent + colMarker + colGap // line 2 starts under the name
	mobileMarksMin   = 36                             // below this width the subagent and badge cells are dropped
)

// renderRowStacked renders a session's two mobile lines.
func renderRowStacked(m Model, s protocol.SessionSummary, width int) (nameLine, textLine string) {
	selected := m.SelectedID == s.ID
	now := m.now()
	retiring := !s.RetireAt.IsZero()
	dim := styleDim
	gap := strings.Repeat(" ", colGap)
	indent, marker, waiting := rowLead(m, s)

	right := gap + cell(dim.Align(lipgloss.Right), durationAgoAt(now, s.LastEventAt), colTime)
	rightW := colGap + colTime
	if width >= mobileMarksMin {
		right = gap + subagentCell(s, colAgents, retiring && !selected) + gap + longRunCell(now, s, colRun) + right
		rightW += colGap + colAgents + colGap + colRun
	}
	icons, iconsW := mobileIcons(m, s, selected)
	right, rightW = icons+right, iconsW+rightW
	nameW := max(width-mobileTextIndent-rightW, 0)
	nameLine = indent + marker + gap + cell(rowNameStyle(s, selected), displayName(s), nameW) + right

	textW := max(width-mobileTextIndent, 0)
	text := rowDescStyle(m, s, selected).Width(textW).
		Render(truncateWords(rowDescText(m, s, textW, waiting), textW))
	textLine = strings.Repeat(" ", mobileTextIndent) + text
	return blinkRow(m, s.ID, nameLine), blinkRow(m, s.ID, textLine)
}

// mobileIcons is line 1's strip with row_icons on: a gap, then the unseen
// mark and the why glyph, one space apart, each one cell and blank when
// empty. A slot row_icon_set removes is left out. "" and 0 with row_icons
// off.
func mobileIcons(m Model, s protocol.SessionSummary, selected bool) (string, int) {
	if !settings.FeatureOn(m.Store, "row_icons") {
		return "", 0
	}
	var out string
	w := 0
	for _, slot := range iconSlots(m) {
		if slot != slotUnseen && slot != slotReason {
			continue
		}
		sep := " "
		if w == 0 {
			sep = strings.Repeat(" ", colGap)
		}
		out += sep + slotCell(m, s, slot, 1, selected)
		w += len(sep) + 1
	}
	return out, w
}
