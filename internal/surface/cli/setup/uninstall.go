package setup

import (
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

var (
	uninstallOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50"))
	uninstallBullet = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
)

// RunUninstall removes rex artifacts with user confirmation per section.
func RunUninstall(args []string) error {
	fs_ := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	yes := fs_.Bool("yes", false, "skip confirmations")
	fs_.BoolVar(yes, "y", false, "skip confirmations (shorthand)")
	binaries := fs_.Bool("binaries", true, "remove rex and rex-daemon binaries")
	wipeState := fs_.Bool("wipe-state", false, "remove ~/.local/state/rex/ and ~/.local/share/rex/sessions/")
	purgeConfig := fs_.Bool("purge-config", false, "remove ~/.config/rex/")
	stripProfile := fs_.Bool("strip-profile", false, "remove # BEGIN REX … # END REX blocks from shell profiles")
	all := fs_.Bool("all", false, "equivalent to all flags above")
	dryRun := fs_.Bool("dry-run", false, "print what would be removed without touching anything")

	if err := fs_.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}

	if *all {
		*binaries = true
		*wipeState = true
		*purgeConfig = true
		*stripProfile = true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, "cannot determine home directory: "+err.Error())
	}

	// Resolve binary paths.
	rexBin, err := os.Executable()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, "cannot resolve rex executable: "+err.Error())
	}
	rexBin, err = filepath.EvalSymlinks(rexBin)
	if err != nil {
		return core.NewExitError(core.ExitGeneric, "cannot resolve rex symlink: "+err.Error())
	}

	rexDaemonBin, _ := exec.LookPath("rex-daemon")

	// State / share / config dirs.
	stateDir := filepath.Join(home, ".local", "state", "rex")
	sessionsDir := filepath.Join(home, ".local", "share", "rex", "sessions")
	configDir := filepath.Join(home, ".config", "rex")

	// Profile files to scan for strip-profile.
	profileFiles := profileFiles(home)

	// ── Preview ──────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  " + core.HelpHRStyle.Render(strings.Repeat("─", 44)))
	fmt.Println("  " + core.HelpBold.Render("Will remove:"))

	if *binaries {
		if *binaries {
			fmt.Println("    " + core.HelpSection.Render("binaries") + "     " + core.HelpCmd.Render(rexBin))
			if rexDaemonBin != "" {
				fmt.Println("                 " + core.HelpCmd.Render(rexDaemonBin))
			} else {
				fmt.Println("                 " + core.HelpDim.Render("rex-daemon  (not found on PATH)"))
			}
		}
	}

	if *wipeState {
		stateInfo := dirSummary(stateDir)
		sessInfo := dirSummary(sessionsDir)
		fmt.Println("    " + core.HelpSection.Render("state") + "        " + core.HelpCmd.Render(stateDir+"/") + core.HelpDim.Render("  "+stateInfo))
		fmt.Println("    " + core.HelpSection.Render("sessions") + "     " + core.HelpCmd.Render(sessionsDir+"/") + core.HelpDim.Render("  "+sessInfo))
	}

	if *purgeConfig {
		fmt.Println("    " + core.HelpSection.Render("config") + "       " + core.HelpCmd.Render(configDir+"/"))
	}

	if *stripProfile {
		found := profileFilesWithBlock(profileFiles)
		if len(found) > 0 {
			for _, p := range found {
				fmt.Println("    " + core.HelpSection.Render("profile") + "      " + core.HelpCmd.Render(p))
			}
		} else {
			fmt.Println("    " + core.HelpSection.Render("profile") + "      " + core.HelpDim.Render("(no # BEGIN REX blocks found)"))
		}
	}

	if *dryRun {
		fmt.Println()
		fmt.Println("  " + uninstallBullet.Render(core.HelpBullet) + " " + core.HelpDim.Render("dry-run: nothing removed"))
		fmt.Println()
		return nil
	}

	fmt.Println()

	// ── Guard: system-location binaries ──────────────────────────────────────
	if *binaries {
		if !strings.HasPrefix(rexBin, home) {
			return core.NewExitError(core.ExitOperationRefused,
				fmt.Sprintf("rex is installed in a system location; remove manually with `sudo rm %s`", rexBin))
		}
		if rexDaemonBin != "" && !strings.HasPrefix(rexDaemonBin, home) {
			return core.NewExitError(core.ExitOperationRefused,
				fmt.Sprintf("rex-daemon is installed in a system location; remove manually with `sudo rm %s`", rexDaemonBin))
		}
	}

	// ── Confirmation ─────────────────────────────────────────────────────────
	confirmAll := *yes || *all

	if !confirmAll {
		if !confirm("  proceed with all removals? [y/N] ") {
			return core.NewExitError(core.ExitGeneric, "aborted")
		}
	} else if !*yes {
		// --all was set but not --yes; single confirmation.
		if !confirm("  proceed? [y/N] ") {
			return core.NewExitError(core.ExitGeneric, "aborted")
		}
	}

	// ── Stop daemon before removing binaries ─────────────────────────────────
	if *binaries {
		stopDaemonQuietly()
	}

	// ── Remove binaries ───────────────────────────────────────────────────────
	if *binaries {
		removed := false
		if err := removeFile(rexBin); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", rexBin, err)
		} else {
			removed = true
			slog.Info("uninstall: removed binary", "path", rexBin)
		}
		if rexDaemonBin != "" {
			if err := removeFile(rexDaemonBin); err != nil {
				fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", rexDaemonBin, err)
			} else {
				removed = true
				slog.Info("uninstall: removed binary", "path", rexDaemonBin)
			}
		}
		if removed {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + core.HelpCmd.Render("binaries removed"))
		}
	}

	// ── Remove state + sessions ───────────────────────────────────────────────
	if *wipeState {
		if err := removeDir(stateDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", stateDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + core.HelpCmd.Render("state removed"))
			slog.Info("uninstall: removed state dir", "path", stateDir)
		}
		if err := removeDir(sessionsDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", sessionsDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + core.HelpCmd.Render("sessions removed"))
			slog.Info("uninstall: removed sessions dir", "path", sessionsDir)
		}
	}

	// ── Remove config ─────────────────────────────────────────────────────────
	if *purgeConfig {
		if err := removeDir(configDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", configDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + core.HelpCmd.Render("config removed"))
			slog.Info("uninstall: removed config dir", "path", configDir)
		}
	}

	// ── Strip profile blocks ──────────────────────────────────────────────────
	if *stripProfile {
		stripped, errs := stripProfileBlocks(profileFiles, time.Now())
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  error stripping profile: %v\n", e)
		}
		for _, s := range stripped {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + core.HelpCmd.Render("profile block removed from "+s.Path) +
				core.HelpDim.Render("  (backup: "+s.Backup+")"))
		}
		if len(stripped) == 0 && len(errs) == 0 {
			fmt.Println("  " + uninstallBullet.Render(core.HelpBullet) + " " + core.HelpDim.Render("no profile blocks found"))
		}
	}

	// ── Hint ──────────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  " + uninstallBullet.Render(core.HelpBullet) + " " + core.HelpDim.Render("run `rm -rf ~/.local/state/rex ~/.config/rex` to be doubly sure"))
	fmt.Println()

	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func confirm(prompt string) bool {
	fmt.Print(prompt)
	var ans string
	_, _ = fmt.Fscanln(os.Stdin, &ans)
	return ans == "y" || ans == "Y" || ans == "yes"
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	return nil
}

// stopDaemonQuietly sends SIGTERM to rex-daemon if found; ignores errors.
func stopDaemonQuietly() {
	pid, err := daemonctl.FindPID()
	if err != nil {
		return
	}
	daemonctl.StopQuietly()
	slog.Info("uninstall: sent SIGTERM to daemon", "pid", pid)
	fmt.Println("  " + uninstallBullet.Render(core.HelpBullet) + " " + core.HelpDim.Render(fmt.Sprintf("sent SIGTERM to rex-daemon (pid %s)", pid)))
}

// dirSummary returns a human-readable "(N files, X KB)" string for a directory.
func dirSummary(dir string) string {
	var count int
	var totalBytes int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best-effort size summary: unreadable entries are skipped, not fatal.
		}
		count++
		if info, e := d.Info(); e == nil {
			totalBytes += info.Size()
		}
		return nil
	})
	if count == 0 {
		return "(empty or not found)"
	}
	kb := totalBytes / 1024
	return fmt.Sprintf("(%d files, %d KB)", count, kb)
}
