package setup

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

var (
	updateOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("#5FC96E"))
	updateBullet = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	updateKey    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6E6E6"))
	updateVal    = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
	updateErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E05C5C"))
)

// updateDeps are RunUpdate's side effects. Tests replace them so the
// ordering of detect, stop and upgrade can be checked without touching a
// real daemon, binary or network.
type updateDeps struct {
	out, errOut      io.Writer
	stdin            io.Reader
	detectMode       func() (installMode, string, error)
	daemon           daemonControl
	goInstall        func(verbose bool) error
	fromSource       func(dir string, verbose bool) error
	installedVersion func() string
}

// defaultUpdateDeps wires RunUpdate to the real system. It fails when the
// daemon socket cannot be resolved (no home directory).
func defaultUpdateDeps() (updateDeps, error) {
	daemon, err := defaultDaemonControl()
	if err != nil {
		return updateDeps{}, err
	}
	return updateDeps{
		out:              os.Stdout,
		errOut:           os.Stderr,
		stdin:            os.Stdin,
		detectMode:       detectInstallMode,
		daemon:           daemon,
		goInstall:        upgradeGoInstall,
		fromSource:       upgradeFromSource,
		installedVersion: readInstalledVersion,
	}, nil
}

// RunUpdate upgrades the rex and rex-daemon binaries in place.
//
// Ordering invariant: everything that can refuse (install-mode detection,
// the binary-swap refusal, the confirmation prompt) runs before the daemon
// is stopped, so a refused or cancelled update never kills the user's
// sessions. The daemon is stopped, and its socket confirmed closed, only
// right before the upgrade itself.
func RunUpdate(args []string) error {
	d, err := defaultUpdateDeps()
	if err != nil {
		return err
	}
	return runUpdate(args, d)
}

func runUpdate(args []string, d updateDeps) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(d.errOut)
	check := fs.Bool("check", false, "print version info and exit without upgrading")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	yShort := fs.Bool("y", false, "skip confirmation prompt (shorthand)")
	source := fs.String("source", "", "explicit source checkout path; run git pull + ./install.sh")
	verbose := fs.Bool("verbose", false, "stream go install output instead of spinning")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}

	slog.Info("update: starting", "check", *check, "source", *source, "verbose", *verbose)

	// --check: print version and bail.
	if *check {
		return runUpdateCheck(d.out)
	}

	mode, exePath, err := d.detectMode()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: could not determine install mode: %v", err))
	}
	if *source == "" && mode == installModeBinarySwap {
		return refuseBinarySwap(d.out, exePath)
	}

	daemonRunning := d.daemon.running()
	printUpdatePlan(d.out, exePath, *source, mode, daemonRunning)

	skipConfirm := *yes || *yShort
	if !skipConfirm && !confirmUpdate(d) {
		slog.Info("update: user declined")
		return core.NewExitError(core.ExitOperationRefused, "upgrade cancelled")
	}

	if daemonRunning {
		stopDaemonForUpdate(d)
	}

	if err := runUpgrade(d, *source, *verbose); err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: %v", err))
	}

	newVer := d.installedVersion()
	fmt.Fprintf(d.out, "\n  %s %s\n", updateOK.Render("✓"), updateKey.Render(fmt.Sprintf("rex %s → %s", core.Version, newVer)))
	if daemonRunning {
		fmt.Fprintf(d.out, "  %s %s\n", updateBullet.Render("·"), updateVal.Render("daemon will restart on next `rex`"))
	}
	fmt.Fprintln(d.out)

	slog.Info("update: complete", "old_version", core.Version, "new_version", newVer)
	return nil
}

// printUpdatePlan shows what the upgrade will do before asking.
func printUpdatePlan(w io.Writer, exePath, source string, mode installMode, daemonRunning bool) {
	fmt.Fprintln(w)
	printKV(w, "current", core.Version)
	printKV(w, "binary", exePath)
	if source != "" {
		printKV(w, "mode", "source checkout (--source)")
		printKV(w, "source", source)
	} else {
		printKV(w, "mode", modeLabel(mode))
	}
	if daemonRunning {
		printKV(w, "daemon", "running (will stop, then restart on next `rex`)")
	} else {
		printKV(w, "daemon", "not running")
	}
	fmt.Fprintln(w)
}

// confirmUpdate asks "upgrade now? [Y/n]"; an empty answer means yes.
func confirmUpdate(d updateDeps) bool {
	fmt.Fprint(d.out, "  upgrade now? [Y/n] ")
	answer, _ := bufio.NewReader(d.stdin).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

// refuseBinarySwap explains that a binary-swap install (anything not under
// a Go bin dir, e.g. install.sh's ~/.local/bin) can't upgrade itself, and
// returns the refusal. It runs before anything is stopped.
func refuseBinarySwap(w io.Writer, exePath string) error {
	fmt.Fprintf(w, "\n  %s\n", updateErr.Render("cannot self-upgrade binary-swap install"))
	fmt.Fprintf(w, "  your rex binary is at: %s\n", updateKey.Render(exePath))
	if repoHint := guessSourceDir(exePath); repoHint != "" {
		fmt.Fprintf(w, "  re-run the checkout's install script to upgrade:\n\n")
		fmt.Fprintf(w, "    %s\n\n", updateKey.Render(fmt.Sprintf("cd %s && ./install.sh", repoHint)))
	} else {
		fmt.Fprintf(w, "  re-run your original install script (e.g. %s) to upgrade.\n\n",
			updateKey.Render("./install.sh"))
	}
	slog.Info("update: binary-swap mode, cannot self-upgrade", "exe", exePath)
	return core.NewExitError(core.ExitOperationRefused, "re-run your install script to upgrade (see above)")
}

// stopDaemonForUpdate stops the daemon and waits for its socket to close,
// so the next `rex` starts the new daemon binary. Failure is only a
// warning: replacing the binaries under a running daemon is still safe on
// Unix, and the old daemon keeps serving until it is restarted.
func stopDaemonForUpdate(d updateDeps) {
	slog.Info("update: stopping daemon before upgrade")
	if err := d.daemon.stopAndWait(); err != nil {
		fmt.Fprintf(d.errOut, "  %s  warning: could not stop daemon: %v\n", updateErr.Render("!"), err)
		slog.Info("update: daemon stop warning", "err", err)
		return
	}
	slog.Info("update: daemon stopped")
}

// runUpgrade runs the source-checkout upgrade when source is set, else go
// install. Binary-swap installs never get here (refuseBinarySwap).
func runUpgrade(d updateDeps, source string, verbose bool) error {
	if source != "" {
		if err := d.fromSource(source, verbose); err != nil {
			slog.Info("update: source upgrade failed", "err", err)
			return err
		}
		return nil
	}
	if err := d.goInstall(verbose); err != nil {
		slog.Info("update: go install failed", "err", err)
		return err
	}
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

// detectInstallMode classifies the running rex binary and returns its
// symlink-resolved path.
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
	gopath := os.Getenv("GOPATH")
	home, err := paths.Home()
	if err != nil && gopath == "" {
		// ~/go/bin cannot be located; a relative "go/bin" would match
		// nothing meaningful.
		return 0, "", err
	}
	return classifyInstall(real, home, gopath), real, nil
}

// classifyInstall reports go-install mode when exe is under $GOPATH/bin
// (default ~/go/bin) or ~/go/bin, and binary-swap mode otherwise.
func classifyInstall(exe, home, gopath string) installMode {
	if gopath == "" {
		gopath = filepath.Join(home, "go")
	}
	for _, binDir := range []string{filepath.Join(gopath, "bin"), filepath.Join(home, "go", "bin")} {
		if strings.HasPrefix(exe, binDir+string(os.PathSeparator)) {
			return installModeGoInstall
		}
	}
	return installModeBinarySwap
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

	if runtime.GOOS == "windows" {
		return runSourceStep(dir, verbose, "install.ps1", "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(dir, "install.ps1"))
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

func runUpdateCheck(w io.Writer) error {
	fmt.Fprintln(w)
	printKV(w, "version", core.Version)
	printKV(w, "remote", "(version check not yet wired — no remote lookup performed)")
	fmt.Fprintln(w)
	slog.Info("update: --check mode, printed version", "version", core.Version)
	return nil
}

// readInstalledVersion runs `rex --version` on the current executable to get
// the post-upgrade version string. Falls back to core.Version constant on error
// (go install replaces the binary, so os.Executable() may now be new).
func readInstalledVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return core.Version
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return core.Version
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return core.Version
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

func printKV(w io.Writer, key, val string) {
	k := fmt.Sprintf("  %-10s", key)
	fmt.Fprintf(w, "%s %s\n", updateKey.Render(k), updateVal.Render(val))
}
