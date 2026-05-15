package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderHelp() string {
	bg := lipgloss.NewStyle().Background(colorBgModal)
	title := bg.Foreground(colorFgDim).Render("HELP")
	rows := []string{
		title,
		"",
		styleSlug.Render("Navigation"),
		"  j k           move row selection",
		"  g G           top / bottom",
		"  1 2 3         jump to Needs input / Working / Completed",
		"  t             cycle tool filter",
		"",
		styleSlug.Render("Actions"),
		"  enter         open modal on selected session",
		"  n             new-agent wizard",
		"  dd            delete selected",
		"  i             focus λ prompt (new-session text)",
		"",
		styleSlug.Render("Modes"),
		"  :             command mode",
		"  ?             this help",
		"  q             quit (no confirm)",
		"",
		styleSlug.Render("Commands"),
		"  :q :quit      quit (confirm)",
		"  :q!           force quit",
		"  :bg :detach   detach, save place",
		"  :new          new-agent wizard",
		"  :filter <t>   set filter chip",
		"  :rm <sel>     delete",
		"  :rename <s> <new>",
		"  :reload       reload tools.yaml",
		"  :help         this overlay",
		"",
		styleDim.Render("esc to close"),
	}
	return strings.Join(rows, "\n")
}
