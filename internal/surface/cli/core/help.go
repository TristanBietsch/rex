package core

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// HelpBold and related styles are shared by help, doctor, and uninstall output.
	HelpBold     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	HelpSection  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	HelpCmd      = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	HelpDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	HelpMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A4F5A"))
	HelpAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B8DEF"))
	HelpHRStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#262A36"))
	helpStarChar = "✦"
	helpArrow    = "▸"
	HelpBullet   = "·"
)

type helpRow struct {
	cmd  string
	desc string
}

type helpSec struct {
	title string
	rows  []helpRow
}

// RunHelp prints the top-level rex help screen.
func RunHelp() error {
	sections := []helpSec{
		{
			title: "Session",
			rows: []helpRow{
				{"ls", "list sessions"},
				{"new", "create a new session (wizard)"},
				{"attach <sel>", "attach to a session (ctrl+] to detach)"},
				{"reply <sel> <text>", "reply to a needs-input session"},
				{"send <sel> <text>", "send raw input"},
				{"wait <sel>", "block until session state changes"},
				{"rename <sel> <slug>", "rename a session"},
				{"rm <sel>", "delete a session"},
				{"archive <sel>", "archive a session"},
				{"complete <sel>", "cleanly terminate a session and mark it done"},
				{"log <sel>", "stream session log"},
			},
		},
		{
			title: "Daemon",
			rows: []helpRow{
				{"status", "aggregate one-liner (pipe-friendly)"},
				{"daemon", "run daemon in foreground"},
				{"reload", "reload tools.yaml"},
			},
		},
		{
			title: "Insights",
			rows: []helpRow{
				{"digest", "today's sessions, time, totals"},
				{"stats", "lifetime usage by model/tool"},
				{"fleet", "list/set/unset session fleets"},
			},
		},
		{
			title: "Lifecycle",
			rows: []helpRow{
				{"setup", "guided first-run wizard"},
				{"doctor", "diagnostic check"},
				{"update", "upgrade rex in place"},
				{"uninstall", "remove rex + optional state"},
			},
		},
		{
			title: "Other",
			rows: []helpRow{
				{"render", "render an event stream"},
				{"config", "view config"},
				{"completion <shell>", "generate shell completions"},
				{"version", "print version"},
			},
		},
		{
			title: "Flags",
			rows: []helpRow{
				{"-h, --help", "show this help"},
				{"-v, --version", "print version"},
			},
		},
	}

	cmdWidth := 0
	for _, sec := range sections {
		for _, r := range sec.rows {
			if w := lipgloss.Width(r.cmd); w > cmdWidth {
				cmdWidth = w
			}
		}
	}
	cmdWidth += 4

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + HelpAccent.Render(helpStarChar) + " " + HelpBold.Render("rex") + HelpDim.Render(" — Runtime Executive for Agents") + "\n")
	b.WriteString("  " + HelpHRStyle.Render(strings.Repeat("─", 44)) + "\n\n")

	b.WriteString("  " + HelpSection.Render("USAGE") + "\n")
	b.WriteString("    " + HelpCmd.Render("rex") + HelpDim.Render("                          launch the TUI") + "\n")
	b.WriteString("    " + HelpCmd.Render("rex <command> [flags]") + "\n\n")

	for _, sec := range sections {
		b.WriteString("  " + HelpSection.Render(strings.ToUpper(sec.title)) + "\n")
		for _, r := range sec.rows {
			pad := strings.Repeat(" ", cmdWidth-lipgloss.Width(r.cmd))
			b.WriteString("    " + HelpAccent.Render(helpArrow) + " " + HelpCmd.Render(r.cmd) + pad + HelpDim.Render(r.desc) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("  " + HelpMuted.Render(HelpBullet) + " " + HelpDim.Render("selectors: short id, slug, ") + HelpCmd.Render("@needs") + HelpDim.Render(", ") + HelpCmd.Render("@working") + HelpDim.Render(", ") + HelpCmd.Render("@done") + "\n")
	b.WriteString("  " + HelpMuted.Render(HelpBullet) + " " + HelpDim.Render("inside the TUI press ") + HelpCmd.Render("?") + HelpDim.Render(" for keybindings") + "\n")
	b.WriteString("\n")

	fmt.Print(b.String())
	return nil
}
