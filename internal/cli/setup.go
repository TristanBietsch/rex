package cli

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/daemonctl"
	"github.com/tristanbietsch/rex/internal/rexlog"
	"github.com/tristanbietsch/rex/internal/tui"
)

// RunSetup implements `rex setup`.
func RunSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	preview := fs.Bool("preview", false, "print plan only, no writes")
	yes := fs.Bool("yes", false, "apply defaults non-interactively")
	fs.Bool("y", false, "alias for --yes") // parsed below via shorthand
	tool := fs.String("tool", "", "override default tool id")
	model := fs.String("model", "", "override default model id")
	effort := fs.String("effort", "", "override default effort")
	noShell := fs.Bool("no-shell", false, "skip shell integration")
	configPath := fs.String("config", defaultConfigPath(), "path to config.yaml")
	socket := fs.String("socket", DefaultSocket(), "daemon socket path")
	force := fs.Bool("force", false, "overwrite existing config without prompting")

	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	// -y is an alias
	shortY := false
	for _, a := range args {
		if a == "-y" || a == "--y" {
			shortY = true
		}
	}
	doYes := *yes || shortY

	rexlog.Init("setup")
	defer rexlog.Close()
	slog.Info("setup: started",
		"preview", *preview, "yes", doYes,
		"tool", *tool, "model", *model, "effort", *effort,
		"no_shell", *noShell, "config", *configPath, "socket", *socket)

	if *preview {
		printPreview(*configPath, *socket, *tool, *model, *effort, *noShell)
		return nil
	}

	if doYes {
		// guard: non-empty existing config requires --force
		if !*force {
			if b, err := os.ReadFile(*configPath); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return NewExitError(ExitInvalidArgs,
					fmt.Sprintf("config already exists at %s — rerun with --force to overwrite", *configPath))
			}
		}
		return tui.ApplyDefaultsNonInteractive(*configPath, *socket, *tool, *model, *effort, *noShell)
	}

	return tui.RunSetupWizard(*configPath, *socket)
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "rex", "config.yaml")
}

// ── Preview renderer (cli-side, uses cli lipgloss palette) ───────────────────

var (
	setupBold    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	setupSection = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E6E6E6"))
	setupCmd     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	setupDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	setupAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5B8DEF"))
	setupHR      = lipgloss.NewStyle().Foreground(lipgloss.Color("#262A36"))
)

func printPreview(configPath, socket, toolID, modelID, effortVal string, noShell bool) {
	slog.Info("setup: preview mode", "config_path", configPath)

	fmt.Println()
	fmt.Println("  " + setupAccent.Render("✦") + " " + setupBold.Render("rex setup --preview"))
	fmt.Println("  " + setupHR.Render(strings.Repeat("─", 44)))
	fmt.Println()

	fmt.Println("  " + setupSection.Render("FILES THAT WOULD BE WRITTEN"))
	fmt.Println()
	fmt.Println("  " + setupAccent.Render("▸") + " " + setupCmd.Render(configPath))
	fmt.Println("    " + setupDim.Render("config.yaml — written via settings.Store.Save() (atomic temp+rename)"))
	fmt.Println()

	if !noShell {
		rcPath := tui.DetectShellRC()
		if rcPath != "" {
			if tui.HasShellBlock(rcPath) {
				fmt.Println("  " + setupAccent.Render("▸") + " " + setupCmd.Render(rcPath) +
					setupDim.Render("  (block already present — rewrite in place)"))
			} else {
				fmt.Println("  " + setupAccent.Render("▸") + " " + setupCmd.Render(rcPath) +
					setupDim.Render("  (append shell block)"))
			}
		} else {
			fmt.Println("  " + setupDim.Render("(no shell RC detected — skip shell integration)"))
		}
	} else {
		fmt.Println("  " + setupDim.Render("(--no-shell: shell integration skipped)"))
	}

	fmt.Println()
	fmt.Println("  " + setupSection.Render("SHELL BLOCK"))
	fmt.Println()
	for _, line := range strings.Split(tui.ShellProfileBlock(filepath.Base(os.Getenv("SHELL"))), "\n") {
		fmt.Println("  " + setupDim.Render(line))
	}

	fmt.Println()
	fmt.Println("  " + setupSection.Render("DAEMON"))
	if daemonctl.Reachable(socket) {
		fmt.Println("  " + setupCmd.Render("reachable") + setupDim.Render(" at "+socket))
	} else {
		fmt.Println("  " + setupDim.Render("not running — interactive mode offers to start it"))
	}

	fmt.Println()
	fmt.Println("  " + setupSection.Render("NEXT COMMAND AFTER SETUP"))
	toolDisplay := toolID
	if toolDisplay == "" {
		toolDisplay = "claude"
	}
	modelDisplay := modelID
	if modelDisplay == "" {
		modelDisplay = "opus"
	}
	nextCmd := fmt.Sprintf(`rex new "your first task" --tool %s --model %s`, toolDisplay, modelDisplay)
	if effortVal != "" {
		nextCmd += " --effort " + effortVal
	}
	fmt.Println("  " + setupCmd.Render(nextCmd))
	fmt.Println()
	fmt.Println("  " + setupDim.Render("Docs: https://github.com/tristanbietsch/rex"))
	fmt.Println()
}
