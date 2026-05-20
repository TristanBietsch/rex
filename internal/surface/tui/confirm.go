package tui

func renderQuitConfirm(width int) string {
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("quit rex? running sessions stay alive in the daemon — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}

// renderDeleteConfirm asks before tearing down a session. Includes the slug so the
// user can see which row they're about to nuke.
func renderDeleteConfirm(m Model, width int) string {
	slug := m.PendingDeleteID
	for _, s := range m.Sessions {
		if s.ID == m.PendingDeleteID {
			slug = s.Slug
			break
		}
	}
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("delete session ") +
		styleSlug.Render(slug) +
		stylePrimary.Render("? this kills any running process and removes the transcript — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}
