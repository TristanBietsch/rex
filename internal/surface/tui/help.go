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
		"  ::            open command bar (q detach · done · kill · name <t>)",
		"  :::           type a literal \"::\" into the agent",
		"  ctrl+pgup     scroll back (primary-screen agents; also wheel up)",
		"                k/j line · pgup/pgdn · ^U/^D half · g top · G/esc live",
		"",
		styleSlug.Render("Long-running badge"),
		"  ▴12m dim · ▴45m amber · ▴3h red · ▴7h red inverse (time working)",
		"",
		styleDim.Render("esc to close"),
	}
	return strings.Join(rows, "\n")
}
