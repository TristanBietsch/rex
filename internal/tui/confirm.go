package tui

import "github.com/charmbracelet/lipgloss"

func renderQuitConfirm() string {
	prompt := styleStateNeeds.Render("?") + " " +
		styleSlug.Render("quit rex? running sessions stay alive in the daemon — ") +
		styleDim.Render("y / N")
	hint := styleDim.Render("y or enter to quit · n or esc to cancel")
	return lipgloss.NewStyle().Padding(1, 2).Render(prompt + "\n" + hint)
}
