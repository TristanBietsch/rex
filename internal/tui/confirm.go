package tui

func renderQuitConfirm(width int) string {
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("quit rex? running sessions stay alive in the daemon — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}
