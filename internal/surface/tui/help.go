package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpLines is the help text between the title and the footer.
func helpLines() []string {
	return []string{
		styleSlug.Render("Navigation"),
		"  j k           move row selection",
		"  g G           top / bottom",
		"  1 2 3         jump to Needs input / Working / Completed",
		"  t             cycle tool filter",
		"",
		styleSlug.Render("Mouse (mouse_enabled)"),
		"  click         select row · filter chip",
		"  double-click  attach to row",
		"  wheel         move row selection",
		"  hold ⌥ / ⇧    select text natively (iTerm2 · Ghostty/kitty)",
		"",
		styleSlug.Render("Actions"),
		"  enter         attach to selected session (qq, ctrl+] or ::q to detach)",
		"  n             new-agent wizard",
		"  r             rename selected (display name; empty = slug)",
		"  cc            mark done / undo (clears after retire_after_hours)",
		"  C             complete now (kill agent, mark done)",
		"  dd            delete selected",
		"  i             focus λ prompt (new-session text)",
		"",
		styleSlug.Render("Modes"),
		"  :             command mode",
		"  ?             this help",
		"",
		styleSlug.Render("Commands"),
		"  :q :quit      quit (confirm)",
		"  :q!           force quit",
		"  :bg :detach   detach, save place",
		"  :new          new-agent wizard",
		"  :filter <t>   set filter chip",
		"  :rm <sel>     delete",
		"  :rename <s> <new-slug>   change slug",
		"  :name [text]  rename selected (no text: open editor)",
		"  :done [sel]   mark done / undo",
		"  :complete [sel]  kill + mark done now",
		"  :reload       reload tools.yaml",
		"  :fail         inspect failed sessions + daemon log",
		"  :stats        session statistics overlay",
		"  :help         this overlay",
		"",
		styleSlug.Render("Inside an attached session"),
		"  ::            open command bar (q detach · done · kill · name <text>)",
		"  :::           type a literal \"::\" into the agent",
		"  ctrl+pgup     scroll back (primary-screen agents; also wheel up)",
		"                k/j line · pgup/pgdn · ^U/^D half · g top · G/esc live",
		"",
		styleSlug.Render("Long-running badge"),
		"  ▴12m dim · ▴45m amber · ▴3h red · ▴7h red inverse (time working)",
	}
}

// helpRows is how many help lines fit on screen: the popup adds the title
// and footer (2 lines each) and the overlay border/padding. 0 = all.
func helpRows(m Model) int {
	_, h := screenSize(m)
	rows := h - overlayChrome - 4
	if rows >= len(helpLines()) {
		return 0
	}
	return max(rows, 3) // windowLines' minimum
}

// scrollHelp moves the help popup delta lines, kept in range for the
// current screen height.
func scrollHelp(m Model, delta int) Model {
	rows := helpRows(m)
	if rows == 0 {
		m.HelpScroll = 0
		return m
	}
	m.HelpScroll = max(0, min(m.HelpScroll+delta, len(helpLines())-rows))
	return m
}

// renderHelp renders the help popup. Taller than the screen it shows a
// scrolled window, so the footer and the popup's bottom border stay on
// screen; centerOverlay used to cut them off below ~61 rows.
func renderHelp(m Model) string {
	bg := lipgloss.NewStyle().Background(colorBgModal)
	title := bg.Foreground(colorFgDim).Render("HELP")
	lines := helpLines()
	footer := styleDim.Render("esc to close")
	if rows := helpRows(m); rows > 0 {
		off := max(0, min(m.HelpScroll, len(lines)-rows))
		// windowLines centers on the focus line; aim it at the window's middle.
		lines = windowLines(lines, off+rows/2, rows)
		footer = styleDim.Render("j/k scroll · pgup/pgdn page · esc to close")
	}
	return title + "\n\n" + strings.Join(lines, "\n") + "\n\n" + footer
}
