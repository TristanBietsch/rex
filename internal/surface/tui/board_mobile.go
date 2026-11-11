package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Mobile rows. A session takes two lines, each exactly width wide:
//
//	▸ ◆  release-notes            ⋔ 2  ▴ 42m  11m
//	     Which feature should lead: search or export…
//
// Line 1 is the indent, state marker, name (the flexible cell), the subagent
// and long-run cells and the age. Line 2 is the text under the name.
const (
	mobileTextIndent = rowIndent + colMarker + colGap // line 2 starts under the name
	mobileMarksMin   = 36                             // below this width the subagent and badge cells are dropped
)

// renderRowStacked renders a session's two mobile lines.
func renderRowStacked(m Model, s protocol.SessionSummary, width int) (nameLine, textLine string) {
	selected := m.SelectedID == s.ID
	now := m.now()
	retiring := !s.RetireAt.IsZero()
	dim := lipgloss.NewStyle().Foreground(colorFgDim)
	gap := strings.Repeat(" ", colGap)
	indent, marker, waiting := rowLead(m, s)

	right := gap + cell(dim.Align(lipgloss.Right), durationAgoAt(now, s.LastEventAt), colTime)
	rightW := colGap + colTime
	if width >= mobileMarksMin {
		right = gap + subagentCell(s, colAgents, retiring && !selected) + gap + longRunCell(now, s, colRun) + right
		rightW += colGap + colAgents + colGap + colRun
	}
	nameW := max(width-mobileTextIndent-rightW, 0)
	nameLine = indent + marker + gap + cell(rowNameStyle(s, selected), displayName(s), nameW) + right

	textW := max(width-mobileTextIndent, 0)
	text := lipgloss.NewStyle().Foreground(rowDescColor(m, s, selected)).Width(textW).
		Render(truncateWords(rowDescText(m, s, textW, waiting), textW))
	textLine = strings.Repeat(" ", mobileTextIndent) + text
	return blinkRow(m, s.ID, nameLine), blinkRow(m, s.ID, textLine)
}
