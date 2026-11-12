package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// panelStyle returns a square-bordered, high-contrast panel style whose total
// rendered width (border included) equals width.
func panelStyle(width int) lipgloss.Style {
	inner := width - 2 // lipgloss Width excludes border, includes padding
	if inner < 0 {
		inner = 0
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBorderFocus).
		Padding(1, 3).
		Width(inner)
}

// sectionHeader renders "── Title ───…" filled to exactly width cells.
func sectionHeader(title string, width int) string {
	lead := styleDim.Render("── ")
	t := styleSlug.Render(title)
	used := 3 + ansi.StringWidth(title) + 1
	fill := width - used
	if fill < 0 {
		fill = 0
	}
	return lead + t + styleDim.Render(" "+strings.Repeat("─", fill))
}

// panelWidth returns min(want, termW-4), floored at minW, and never wider
// than termW-2: a panel that does not fit the screen is clipped by the
// terminal, so the floor gives way.
func panelWidth(termW, want, minW int) int {
	w := min(want, termW-4)
	w = max(w, minW)
	return max(min(w, termW-2), 1)
}
