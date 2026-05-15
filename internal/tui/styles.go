package tui

import "github.com/charmbracelet/lipgloss"

// Palette mirrors docs/design.md.
var (
	colorBgBase    = lipgloss.Color("#0F1115")
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
	styleDim          = lipgloss.NewStyle().Foreground(colorFgDim)
	styleMuted        = lipgloss.NewStyle().Foreground(colorFgMuted)
	styleSelected     = lipgloss.NewStyle().Background(colorBgElev).Foreground(colorFgPrimary)

	styleStateWorking = lipgloss.NewStyle().Bold(true).Foreground(colorWorking)
	styleStateNeeds   = lipgloss.NewStyle().Bold(true).Foreground(colorNeeds)
	styleStateDone    = lipgloss.NewStyle().Bold(true).Foreground(colorDone)
	styleStateFailed  = lipgloss.NewStyle().Bold(true).Foreground(colorFailed)
	styleStateCrashed = lipgloss.NewStyle().Bold(true).Foreground(colorCrashed)
)

// Silence unused warnings for fields referenced in later tasks.
var _ = colorBgModal
var _ = colorBorder
