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

// panelLines draws a square, titled panel: the title is set into the top
// border (┌─ HELP ────── rex v1 ─┐) and right, when it fits, sits at the far
// end. content lines get padX columns and padY rows of padding. A width of 0
// sizes the panel to its content; otherwise the panel is exactly width cells
// and wider lines are cut with "…". It returns the box lines and their width.
// Like overlayBoxPad it measures each line once, so overlays stay cheap.
func panelLines(title, right string, content []string, padX, padY, width int) ([]string, int) {
	widths := make([]int, len(content))
	inner := 0
	for i, l := range content {
		widths[i] = ansi.StringWidth(l)
		inner = max(inner, widths[i])
	}
	if width <= 0 {
		need := ansi.StringWidth(title) + 3
		if right != "" {
			need += ansi.StringWidth(right) + 3
		}
		width = max(inner+2*padX, need) + 2
	}
	width = max(width, 4)
	room := max(width-2-2*padX, 0)
	for i := range content {
		if widths[i] > room {
			content[i] = ansi.Truncate(content[i], room, "…")
			widths[i] = ansi.StringWidth(content[i])
		}
	}

	side := styleBorderFocus.Render("│")
	pad := strings.Repeat(" ", padX)
	blank := side + strings.Repeat(" ", width-2) + side
	out := make([]string, 0, len(content)+2+2*padY)
	out = append(out, panelTop(title, right, width-2))
	for range padY {
		out = append(out, blank)
	}
	for i, l := range content {
		out = append(out, side+pad+l+strings.Repeat(" ", room-widths[i])+pad+side)
	}
	for range padY {
		out = append(out, blank)
	}
	out = append(out, styleBorderFocus.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return out, width
}

// panelTop is the top border for a panel with inner cells between the
// corners. The right label is dropped when it does not fit beside the title,
// and the title is cut when it alone does not fit.
func panelTop(title, right string, inner int) string {
	if inner < 4 {
		return styleBorderFocus.Render("┌" + strings.Repeat("─", max(inner, 0)) + "┐")
	}
	if ansi.StringWidth(title) > inner-3 {
		title = ansi.Truncate(title, inner-3, "…")
	}
	lw := ansi.StringWidth(title) + 3
	left := styleBorderFocus.Render("─ ") + styleKey.Render(title) + styleBorderFocus.Render(" ")
	rt, rw := "", 0
	if right != "" && lw+ansi.StringWidth(right)+3 <= inner {
		rt = styleBorderFocus.Render(" ") + styleDim.Render(right) + styleBorderFocus.Render(" ─")
		rw = ansi.StringWidth(right) + 3
	}
	fill := inner - lw - rw
	return styleBorderFocus.Render("┌") + left + styleBorderFocus.Render(strings.Repeat("─", fill)) + rt + styleBorderFocus.Render("┐")
}

// splitTitle takes an overlay's first line as its title (plain text, for
// the panel's top border) and returns the rest as the body.
func splitTitle(content string) (title string, body []string) {
	lines := strings.Split(content, "\n")
	return ansi.Strip(lines[0]), lines[1:]
}

// dimBoard recolors the board behind a non-attach overlay in the muted
// color, so the overlay reads first. It strips the board's styling rather
// than adding any, and is done once per frame; the attach popup skips it to
// keep its frame time.
func dimBoard(bg string) string {
	lines := strings.Split(bg, "\n")
	for i, l := range lines {
		lines[i] = styleMuted.Render(ansi.Strip(l))
	}
	return strings.Join(lines, "\n")
}
