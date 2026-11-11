package tui

import (
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func renderQuitConfirm(width int) string {
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("quit rex? running sessions stay alive in the daemon — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}

// renderDeleteConfirm asks before tearing down a session. Includes the slug so the
// user can see which row they're about to nuke.
func renderDeleteConfirm(m Model, width int) string {
	if n := len(m.PendingPrune); n > 0 {
		// :prune ignores the board's filters: say so, so nobody reads the
		// count as "what is visible".
		scope := ""
		if rowFilterOf(m).rowsActive() || len(m.Hidden) > 0 {
			scope = " (all Done sessions, filters ignored)"
		}
		line := styleArrowYellow.Render("?") + " " +
			stylePrimary.Render(fmt.Sprintf("clear %d completed%s? kills their agents and removes their transcripts%s — ",
				n, scope, PruneKeptNote(m.PendingPruneKept))) +
			styleDim.Render("y / N")
		return padLine(" "+line, width)
	}
	slug := m.PendingDeleteID
	for _, s := range m.Sessions {
		if s.ID == m.PendingDeleteID {
			slug = s.Slug
			break
		}
	}
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("delete session ") +
		styleSlug.Render(protocol.FlattenDisplayText(slug)) +
		stylePrimary.Render("? this kills any running process and removes the transcript — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}
