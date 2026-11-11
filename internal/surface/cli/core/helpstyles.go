package core

import "github.com/charmbracelet/lipgloss"

// Styles still used by doctor and uninstall until they render through the
// kit. They are removed with the setup migration.
var (
	HelpBold    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	HelpSection = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	HelpCmd     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	HelpDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	HelpMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A4F5A"))
	HelpAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B8DEF"))
	HelpHRStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#262A36"))
	HelpBullet  = "·"
)
