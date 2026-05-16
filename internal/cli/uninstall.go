package cli

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	uninstallOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50"))
	uninstallBullet  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	uninstallWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFC107"))
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
		return NewExitError(ExitInvalidArgs, err.Error())
	}

	if *all {
		*binaries = true
		*wipeState = true
		*purgeConfig = true
		*stripProfile = true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return NewExitError(ExitGeneric, "cannot determine home directory: "+err.Error())
	}

	// Resolve binary paths.
	rexBin, err := os.Executable()
	if err != nil {
		return NewExitError(ExitGeneric, "cannot resolve rex executable: "+err.Error())
	}
	rexBin, err = filepath.EvalSymlinks(rexBin)
	if err != nil {
		return NewExitError(ExitGeneric, "cannot resolve rex symlink: "+err.Error())
	}

	rexDaemonBin, _ := exec.LookPath("rex-daemon")

	// State / share / config dirs.
	stateDir := filepath.Join(home, ".local", "state", "rex")
	sessionsDir := filepath.Join(home, ".local", "share", "rex", "sessions")
	configDir := filepath.Join(home, ".config", "rex")

	// Profile files to scan for strip-profile.
	profileFiles := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}

	// ── Preview ──────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  " + helpHRStyle.Render(strings.Repeat("─", 44)))
	fmt.Println("  " + helpBold.Render("Will remove:"))

	if *binaries {
		if *binaries {
			fmt.Println("    " + helpSection.Render("binaries") + "     " + helpCmd.Render(rexBin))
			if rexDaemonBin != "" {
				fmt.Println("                 " + helpCmd.Render(rexDaemonBin))
			} else {
				fmt.Println("                 " + helpDim.Render("rex-daemon  (not found on PATH)"))
			}
		}
	}

	if *wipeState {
		stateInfo := dirSummary(stateDir)
		sessInfo := dirSummary(sessionsDir)
		fmt.Println("    " + helpSection.Render("state") + "        " + helpCmd.Render(stateDir+"/") + helpDim.Render("  "+stateInfo))
		fmt.Println("    " + helpSection.Render("sessions") + "     " + helpCmd.Render(sessionsDir+"/") + helpDim.Render("  "+sessInfo))
	}

	if *purgeConfig {
		fmt.Println("    " + helpSection.Render("config") + "       " + helpCmd.Render(configDir+"/"))
	}

	if *stripProfile {
		found := profileFilesWithBlock(profileFiles)
		if len(found) > 0 {
			for _, p := range found {
				fmt.Println("    " + helpSection.Render("profile") + "      " + helpCmd.Render(p))
			}
		} else {
			fmt.Println("    " + helpSection.Render("profile") + "      " + helpDim.Render("(no # BEGIN REX blocks found)"))
		}
	}

	if *dryRun {
		fmt.Println()
		fmt.Println("  " + uninstallBullet.Render(helpBullet) + " " + helpDim.Render("dry-run: nothing removed"))
		fmt.Println()
		return nil
	}

	fmt.Println()

	// ── Guard: system-location binaries ──────────────────────────────────────
	if *binaries {
		if !strings.HasPrefix(rexBin, home) {
			return NewExitError(ExitOperationRefused,
				fmt.Sprintf("rex is installed in a system location; remove manually with `sudo rm %s`", rexBin))
		}
		if rexDaemonBin != "" && !strings.HasPrefix(rexDaemonBin, home) {
			return NewExitError(ExitOperationRefused,
				fmt.Sprintf("rex-daemon is installed in a system location; remove manually with `sudo rm %s`", rexDaemonBin))
		}
	}

	// ── Confirmation ─────────────────────────────────────────────────────────
	confirmAll := *yes || *all

	if !confirmAll {
		if !confirm("  proceed with all removals? [y/N] ") {
			return NewExitError(ExitGeneric, "aborted")
		}
	} else if !*yes {
		// --all was set but not --yes; single confirmation.
		if !confirm("  proceed? [y/N] ") {
			return NewExitError(ExitGeneric, "aborted")
		}
	}

	// ── Stop daemon before removing binaries ─────────────────────────────────
	if *binaries {
		stopDaemonQuietly()
	}

	// ── Remove binaries ───────────────────────────────────────────────────────
	if *binaries {
		if !confirmAll && !*yes {
			// already confirmed above; no per-section prompt when not --all
		}
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
			fmt.Println("  " + uninstallOK.Render("✓") + " " + helpCmd.Render("binaries removed"))
		}
	}

	// ── Remove state + sessions ───────────────────────────────────────────────
	if *wipeState {
		if err := removeDir(stateDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", stateDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + helpCmd.Render("state removed"))
			slog.Info("uninstall: removed state dir", "path", stateDir)
		}
		if err := removeDir(sessionsDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", sessionsDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + helpCmd.Render("sessions removed"))
			slog.Info("uninstall: removed sessions dir", "path", sessionsDir)
		}
	}

	// ── Remove config ─────────────────────────────────────────────────────────
	if *purgeConfig {
		if err := removeDir(configDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error removing %s: %v\n", configDir, err)
		} else {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + helpCmd.Render("config removed"))
			slog.Info("uninstall: removed config dir", "path", configDir)
		}
	}

	// ── Strip profile blocks ──────────────────────────────────────────────────
	if *stripProfile {
		count, errs := stripProfileBlocks(profileFiles)
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  error stripping profile: %v\n", e)
		}
		if count > 0 {
			fmt.Println("  " + uninstallOK.Render("✓") + " " + helpCmd.Render(fmt.Sprintf("profile blocks removed (%d file(s) modified)", count)))
			slog.Info("uninstall: stripped profile blocks", "files_modified", count)
		} else if len(errs) == 0 {
			fmt.Println("  " + uninstallBullet.Render(helpBullet) + " " + helpDim.Render("no profile blocks found"))
		}
	}

	// ── Hint ──────────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  " + uninstallBullet.Render(helpBullet) + " " + helpDim.Render("run `rm -rf ~/.local/state/rex ~/.config/rex` to be doubly sure"))
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
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return
	}
	if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
		slog.Info("uninstall: could not stop daemon", "pid", pid, "err", err)
		return
	}
	slog.Info("uninstall: sent SIGTERM to daemon", "pid", pid)
	fmt.Println("  " + uninstallBullet.Render(helpBullet) + " " + helpDim.Render(fmt.Sprintf("sent SIGTERM to rex-daemon (pid %s)", pid)))
}

// dirSummary returns a human-readable "(N files, X KB)" string for a directory.
func dirSummary(dir string) string {
	var count int
	var totalBytes int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
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

// profileFilesWithBlock returns only the files that contain a BEGIN REX block.
func profileFilesWithBlock(files []string) []string {
	var found []string
	for _, f := range files {
		if hasProfileBlock(f) {
			found = append(found, f)
		}
	}
	return found
}

// hasProfileBlock reports whether a file contains a # BEGIN REX marker.
func hasProfileBlock(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "# begin rex")
}

// stripProfileBlocks rewrites each file in paths, removing lines bracketed by
// # BEGIN REX … # END REX (case-insensitive, inclusive). Returns count of
// modified files and any errors encountered.
func stripProfileBlocks(paths []string) (int, []error) {
	var modified int
	var errs []error
	for _, p := range paths {
		ok, err := stripBlockFromFile(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		if ok {
			modified++
			slog.Info("uninstall: stripped profile block", "file", p)
		}
	}
	return modified, errs
}

// stripBlockFromFile removes BEGIN REX … END REX blocks from a single file
// using an atomic temp-file + rename. Returns true if the file was modified.
func stripBlockFromFile(path string) (modified bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	inBlock := false
	for scanner.Scan() {
		line := scanner.Text()
		lower := strings.ToLower(strings.TrimSpace(line))
		if !inBlock && lower == "# begin rex" {
			inBlock = true
			modified = true
			continue
		}
		if inBlock {
			if lower == "# end rex" {
				inBlock = false
			}
			continue
		}
		out.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	if !modified {
		return false, nil
	}

	// Atomic write: temp file in same dir, then rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rex-uninstall-*")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err = tmp.Close(); err != nil {
		return false, err
	}

	// Preserve original permissions.
	if info, statErr := os.Stat(path); statErr == nil {
		_ = os.Chmod(tmpName, info.Mode())
	}

	if err = os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}
