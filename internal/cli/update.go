package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/daemonctl"
)

var (
	updateOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("#5FC96E"))
	updateBullet = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	updateKey  = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	updateVal  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	updateErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("#E05C5C"))
)

// RunUpdate upgrades the rex and rex-daemon binaries in place.
func RunUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check   := fs.Bool("check", false, "print version info and exit without upgrading")
	yes     := fs.Bool("yes", false, "skip confirmation prompt")
	yShort  := fs.Bool("y", false, "skip confirmation prompt (shorthand)")
	source  := fs.String("source", "", "explicit source checkout path; run git pull + ./install.sh")
	verbose := fs.Bool("verbose", false, "stream go install output instead of spinning")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	skipConfirm := *yes || *yShort

	slog.Info("update: starting", "check", *check, "source", *source, "verbose", *verbose)

	// --check: print version and bail.
	if *check {
		return runUpdateCheck()
	}

	// Detect install mode.
	mode, exePath, err := detectInstallMode()
	if err != nil {
		return NewExitError(ExitGeneric, fmt.Sprintf("update: could not determine install mode: %v", err))
	}

	// Daemon state.
	socket := DefaultSocket()
	daemonRunning := daemonctl.Reachable(socket)

	// Print plan.
	fmt.Println()
	printKV("current", version)
	printKV("binary", exePath)
	if *source != "" {
		printKV("mode", "source checkout (--source)")
		printKV("source", *source)
	} else {
		printKV("mode", modeLabel(mode))
	}
	if daemonRunning {
		printKV("daemon", "running (will stop, then restart on next `rex`)")
	} else {
		printKV("daemon", "not running")
	}
	fmt.Println()

	if !skipConfirm {
		fmt.Print("  upgrade now? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "" && answer != "y" && answer != "yes" {
			slog.Info("update: user declined")
			return NewExitError(ExitOperationRefused, "upgrade cancelled")
		}
	}

	// Stop daemon if running.
	if daemonRunning {
		slog.Info("update: stopping daemon before upgrade")
		if err := stopDaemonForUpdate(); err != nil {
			// Non-fatal: warn but continue — binary swap is still safe.
			fmt.Fprintf(os.Stderr, "  %s  warning: could not stop daemon: %v\n", updateErr.Render("!"), err)
			slog.Info("update: daemon stop warning", "err", err)
		} else {
			slog.Info("update: daemon stopped")
			// Brief pause so the socket releases.
			time.Sleep(300 * time.Millisecond)
		}
	}

	// Run the upgrade.
	if *source != "" {
		if err := upgradeFromSource(*source, *verbose); err != nil {
			slog.Info("update: source upgrade failed", "err", err)
			return NewExitError(ExitGeneric, fmt.Sprintf("update: %v", err))
		}
	} else {
		switch mode {
		case installModeGoInstall:
			if err := upgradeGoInstall(*verbose); err != nil {
				slog.Info("update: go install failed", "err", err)
				return NewExitError(ExitGeneric, fmt.Sprintf("update: %v", err))
			}
		case installModeBinarySwap:
			// Cannot self-upgrade; print actionable message.
			repoHint := guessSourceDir(exePath)
			if repoHint != "" {
				fmt.Printf("\n  %s\n", updateErr.Render("cannot self-upgrade binary-swap install"))
				fmt.Printf("  your rex binary is at: %s\n", updateKey.Render(exePath))
				fmt.Printf("  re-run the checkout's install script to upgrade:\n\n")
				fmt.Printf("    %s\n\n", updateKey.Render(fmt.Sprintf("cd %s && ./install.sh", repoHint)))
			} else {
				fmt.Printf("\n  %s\n", updateErr.Render("cannot self-upgrade binary-swap install"))
				fmt.Printf("  your rex binary is at: %s\n", updateKey.Render(exePath))
				fmt.Printf("  re-run your original install script (e.g. %s) to upgrade.\n\n",
					updateKey.Render("./install.sh"))
			}
			slog.Info("update: binary-swap mode, cannot self-upgrade", "exe", exePath)
			return NewExitError(ExitOperationRefused, "re-run your install script to upgrade (see above)")
		}
	}

	// Print new version.
	newVer := readInstalledVersion()
	fmt.Printf("\n  %s %s\n", updateOK.Render("✓"), updateKey.Render(fmt.Sprintf("rex %s → %s", version, newVer)))

	if daemonRunning {
		fmt.Printf("  %s %s\n", updateBullet.Render("·"), updateVal.Render("daemon will restart on next `rex`"))
	}
	fmt.Println()

	slog.Info("update: complete", "old_version", version, "new_version", newVer)
	return nil
}

// ---------------------------------------------------------------------------
// Install-mode detection
// ---------------------------------------------------------------------------

type installMode int

const (
	installModeGoInstall  installMode = iota
	installModeBinarySwap installMode = iota
)

func detectInstallMode() (installMode, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	// Resolve symlinks so we compare real paths.
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		real = exe
	}

	home, _ := os.UserHomeDir()
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(home, "go")
	}

	gopathBin := filepath.Join(gopath, "bin")
	homeGoBin := filepath.Join(home, "go", "bin")

	for _, prefix := range []string{gopathBin, homeGoBin} {
		if strings.HasPrefix(real, prefix+string(os.PathSeparator)) || real == filepath.Join(prefix, "rex") {
			return installModeGoInstall, real, nil
		}
	}
	return installModeBinarySwap, real, nil
}

func modeLabel(m installMode) string {
	switch m {
	case installModeGoInstall:
		return "go install"
	default:
		return "binary swap (manual install)"
	}
}

// ---------------------------------------------------------------------------
// Daemon stop (mirrors daemonStop in daemon.go)
// ---------------------------------------------------------------------------

func stopDaemonForUpdate() error {
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return fmt.Errorf("no rex-daemon process found")
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return fmt.Errorf("no rex-daemon process found")
	}
	if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
		return fmt.Errorf("kill -TERM %s: %w", pid, err)
	}
	slog.Info("update: sent SIGTERM to daemon", "pid", pid)
	return nil
}

// ---------------------------------------------------------------------------
// Upgrade strategies
// ---------------------------------------------------------------------------

const goInstallTimeout = 3 * time.Minute

var goInstallTargets = []string{
	"github.com/tristanbietsch/rex/cmd/rex@latest",
	"github.com/tristanbietsch/rex/cmd/rex-daemon@latest",
}

func upgradeGoInstall(verbose bool) error {
	for _, target := range goInstallTargets {
		slog.Info("update: go install", "target", target)
		ctx, cancel := context.WithTimeout(context.Background(), goInstallTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "go", "install", target)
		if verbose {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Printf("  %s go install %s\n", updateBullet.Render("·"), target)
		} else {
			// Capture output; surface only on error.
			combined := new(strings.Builder)
			cmd.Stdout = combined
			cmd.Stderr = combined
			fmt.Printf("  %s installing %s ...\n", updateBullet.Render("·"), target)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("go install %s: %w\n%s", target, err, combined.String())
			}
			continue
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go install %s: %w", target, err)
		}
	}
	return nil
}

func upgradeFromSource(dir string, verbose bool) error {
	slog.Info("update: source upgrade", "dir", dir)

	if err := runSourceStep(dir, verbose, "git pull", "git", "-C", dir, "pull"); err != nil {
		return err
	}

	script := filepath.Join(dir, "install.sh")
	return runSourceStep(dir, verbose, "./install.sh", script)
}

// runSourceStep runs a single source-upgrade step with optional verbose output.
func runSourceStep(dir string, verbose bool, label string, name string, extraArgs ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), goInstallTimeout)
	defer cancel()
	allArgs := extraArgs
	cmd := exec.CommandContext(ctx, name, allArgs...)
	cmd.Dir = dir
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf("  %s %s\n", updateBullet.Render("·"), label)
	} else {
		combined := new(strings.Builder)
		cmd.Stdout = combined
		cmd.Stderr = combined
		fmt.Printf("  %s %s ...\n", updateBullet.Render("·"), label)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s in %s: %w\n%s", label, dir, err, combined.String())
		}
		return nil
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s in %s: %w", label, dir, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func runUpdateCheck() error {
	fmt.Println()
	printKV("version", version)
	printKV("remote", "(version check not yet wired — no remote lookup performed)")
	fmt.Println()
	slog.Info("update: --check mode, printed version", "version", version)
	return nil
}

// readInstalledVersion runs `rex --version` on the current executable to get
// the post-upgrade version string. Falls back to version constant on error
// (go install replaces the binary, so os.Executable() may now be new).
func readInstalledVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return version
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return version
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return version
	}
	return v
}

// guessSourceDir attempts to infer a source checkout from the binary's
// directory by walking up looking for install.sh + go.mod. Best-effort only.
func guessSourceDir(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(filepath.Join(dir, "install.sh")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func printKV(key, val string) {
	k := fmt.Sprintf("  %-10s", key)
	fmt.Printf("%s %s\n", updateKey.Render(k), updateVal.Render(val))
}
