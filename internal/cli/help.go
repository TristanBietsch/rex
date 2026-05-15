package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	helpBold     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	helpSection  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	helpCmd      = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	helpDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	helpMuted    = lipgloss.NewStyle().Foreground(lipgloss.Color("#4A4F5A"))
	helpAccent   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B8DEF"))
	helpHRStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#262A36"))
	helpStarChar = "✦"
	helpArrow    = "▸"
	helpBullet   = "·"
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
	b.WriteString("  " + helpAccent.Render(helpStarChar) + " " + helpBold.Render("rex") + helpDim.Render(" — agent session board") + "\n")
	b.WriteString("  " + helpHRStyle.Render(strings.Repeat("─", 44)) + "\n\n")

	b.WriteString("  " + helpSection.Render("USAGE") + "\n")
	b.WriteString("    " + helpCmd.Render("rex") + helpDim.Render("                          launch the TUI") + "\n")
	b.WriteString("    " + helpCmd.Render("rex <command> [flags]") + "\n\n")

	for _, sec := range sections {
		b.WriteString("  " + helpSection.Render(strings.ToUpper(sec.title)) + "\n")
		for _, r := range sec.rows {
			pad := strings.Repeat(" ", cmdWidth-lipgloss.Width(r.cmd))
			b.WriteString("    " + helpAccent.Render(helpArrow) + " " + helpCmd.Render(r.cmd) + pad + helpDim.Render(r.desc) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("  " + helpMuted.Render(helpBullet) + " " + helpDim.Render("selectors: short id, slug, ") + helpCmd.Render("@needs") + helpDim.Render(", ") + helpCmd.Render("@working") + helpDim.Render(", ") + helpCmd.Render("@done") + "\n")
	b.WriteString("  " + helpMuted.Render(helpBullet) + " " + helpDim.Render("inside the TUI press ") + helpCmd.Render("?") + helpDim.Render(" for keybindings") + "\n")
	b.WriteString("\n")

	fmt.Print(b.String())
	return nil
}
