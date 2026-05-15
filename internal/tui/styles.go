package tui

import "github.com/charmbracelet/lipgloss"

// Palette mirrors docs/design.md and the HTML mockup.
// Foreground colors only; we let the terminal background show through.
var (
	colorBgElev    = lipgloss.Color("#171922")
	colorBgModal   = lipgloss.Color("#1B1E29")
	colorFgPrimary = lipgloss.Color("#E6E6E6")
	colorFgDim     = lipgloss.Color("#7A7F8C")
	colorFgMuted   = lipgloss.Color("#4A4F5A")
	colorBorder    = lipgloss.Color("#262A36")

	colorWorking = lipgloss.Color("#5B8DEF")
	colorNeeds   = lipgloss.Color("#E5B341")
	colorDone    = lipgloss.Color("#4ADE80")
	colorFailed  = lipgloss.Color("#EF4444")
	colorCrashed = lipgloss.Color("#7A7F8C")
)

var (
	styleHeaderApp    = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleHeaderMeta   = lipgloss.NewStyle().Foreground(colorFgDim)
	styleSectionTitle = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	styleSlug         = lipgloss.NewStyle().Bold(true).Foreground(colorFgPrimary)
	stylePrimary      = lipgloss.NewStyle().Foreground(colorFgPrimary)
	styleDim          = lipgloss.NewStyle().Foreground(colorFgDim)
	styleMuted        = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleBorderFg     = lipgloss.NewStyle().Foreground(colorBorder)
	styleSelected     = lipgloss.NewStyle().Background(colorBgElev).Foreground(colorFgPrimary)

	styleStateWorking = lipgloss.NewStyle().Bold(true).Foreground(colorWorking)
	styleStateNeeds   = lipgloss.NewStyle().Bold(true).Foreground(colorNeeds)
	styleStateDone    = lipgloss.NewStyle().Bold(true).Foreground(colorDone)
	styleStateFailed  = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)
	styleStateCrashed = lipgloss.NewStyle().Bold(true).Foreground(colorCrashed)

	styleNewBtn     = lipgloss.NewStyle().Foreground(colorFgDim)
	styleChipActive = lipgloss.NewStyle().Foreground(colorFgPrimary)
	styleChipDim    = lipgloss.NewStyle().Foreground(colorFgDim)
	styleChipSep    = lipgloss.NewStyle().Foreground(colorFgMuted)
)

// renderHR renders a horizontal rule line of given width.
func renderHR(width int) string {
	if width <= 0 {
		return ""
	}
	line := ""
	for i := 0; i < width; i++ {
		line += "─"
	}
	return styleBorderFg.Render(line)
}

// padLine pads s to exactly width visible columns (right-padded with spaces).
// No background is applied — the terminal's native bg shows through.
func padLine(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
