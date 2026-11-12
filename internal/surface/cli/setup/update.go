package setup

import (
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

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
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
	p := core.Out()
	return updateDeps{
		out:              p.Out,
		errOut:           p.Err,
		stdin:            p.In,
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
	out := core.AddOutputFlags(fs, core.AllFormats)
	check := fs.Bool("check", false, "print version info and exit without upgrading")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	yShort := fs.Bool("y", false, "skip confirmation prompt (shorthand)")
	source := fs.String("source", "", "explicit source checkout path; run git pull + ./install.sh --from-source")
	verbose := fs.Bool("verbose", false, "stream go install output instead of spinning")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	// The result goes to d.out, progress, the plan and the question to d.errOut.
	p, err := core.Out().WithIO(d.out, d.errOut, d.stdin)
	if err != nil {
		return err
	}

	slog.Info("update: starting", "check", *check, "source", *source, "verbose", *verbose)

	// -check: print version and bail.
	if *check {
		return runUpdateCheck(p)
	}

	mode, exePath, err := d.detectMode()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: could not determine install mode: %v", err))
	}
	if *source == "" && mode == installModeBinarySwap {
		return refuseBinarySwap(exePath)
	}

	daemonRunning := d.daemon.running()
	printUpdatePlan(p, exePath, *source, mode, daemonRunning)

	skipConfirm := *yes || *yShort
	if !skipConfirm {
		ok, err := p.Confirm("upgrade now?", core.Refusal{Cmd: "update", Flag: "-yes", What: "upgrade",
			Hint: "rex update -yes", DefaultYes: true})
		if err != nil {
			return err
		}
		if !ok {
			slog.Info("update: user declined")
			return core.Errorf(core.ExitOperationRefused, "", "upgrade cancelled")
		}
	}

	if daemonRunning {
		stopDaemonForUpdate(p, d)
	}

	if err := runUpgrade(d, *source, *verbose); err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: %v", err))
	}

	newVer := d.installedVersion()
	slog.Info("update: complete", "old_version", core.Version, "new_version", newVer)
	v := core.ActionView(core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("rex %s → %s", core.Version, newVer)},
		core.Field{Key: "from", Val: core.Version}, core.Field{Key: "to", Val: newVer},
		core.Field{Key: "daemon_restart", Val: daemonRunning})
	if daemonRunning {
		v = append(v, core.Note{Text: "daemon will restart on next `rex`"})
	}
	return p.Emit(core.Result{Cmd: "update", Data: updateData{From: core.Version, To: newVer, DaemonRestart: daemonRunning}, View: v})
}

// updateData is the `-o json` payload of rex update.
type updateData struct {
	From          string `json:"from"`
	To            string `json:"to"`
	DaemonRestart bool   `json:"daemon_restart"`
}

// printUpdatePlan shows what the upgrade will do before asking, on stderr in
// the text format only (it is part of the question, not of the result).
func printUpdatePlan(p *core.Printer, exePath, source string, mode installMode, daemonRunning bool) {
	if p.Format != core.FormatText {
		return
	}
	pairs := []core.Pair{
		{Label: "current", Val: core.Cell{Text: core.Version}},
		{Label: "binary", Val: core.Cell{Text: exePath}},
	}
	if source != "" {
		pairs = append(pairs, core.Pair{Label: "mode", Val: core.Cell{Text: "source checkout (-source)"}},
			core.Pair{Label: "source", Val: core.Cell{Text: source}})
	} else {
		pairs = append(pairs, core.Pair{Label: "mode", Val: core.Cell{Text: modeLabel(mode)}})
	}
	daemon := "not running"
	if daemonRunning {
		daemon = "running (will stop, then restart on next `rex`)"
	}
	pairs = append(pairs, core.Pair{Label: "daemon", Val: core.Cell{Text: daemon}})
	p.EmitErr(core.View{core.KV{Pairs: pairs}})
}

// installerHint is the command that installs the latest release over a
// binary-swap install (install.sh's default ~/.local/bin).
const installerHint = "curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh && sh install.sh"

// refuseBinarySwap explains that a binary-swap install (anything not under
// a Go bin dir, e.g. install.sh's ~/.local/bin) can't upgrade itself, and
// returns the refusal. It runs before anything is stopped.
func refuseBinarySwap(exePath string) error {
	slog.Info("update: binary-swap mode, cannot self-upgrade", "exe", exePath)
	if repoHint := guessSourceDir(exePath); repoHint != "" {
		return core.Errorf(core.ExitOperationRefused, fmt.Sprintf("cd %s && git pull && ./install.sh --from-source", repoHint),
			"cannot self-upgrade binary-swap install (rex is at %s); rebuild from the checkout", exePath)
	}
	return core.Errorf(core.ExitOperationRefused, installerHint,
		"cannot self-upgrade binary-swap install (rex is at %s); re-run the installer to get the latest release", exePath)
}

// stopDaemonForUpdate stops the daemon and waits for its socket to close,
// so the next `rex` starts the new daemon binary. Failure is only a
// warning: replacing the binaries under a running daemon is still safe on
// Unix, and the old daemon keeps serving until it is restarted.
func stopDaemonForUpdate(p *core.Printer, d updateDeps) {
	slog.Info("update: stopping daemon before upgrade")
	if err := d.daemon.stopAndWait(); err != nil {
		p.Warn(fmt.Sprintf("could not stop daemon: %v", err))
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
			cmd.Stdout = core.Out().Err
			cmd.Stderr = core.Out().Err
			core.Out().Progress("go install " + target)
		} else {
			// Capture output; surface only on error.
			combined := new(strings.Builder)
			cmd.Stdout = combined
			cmd.Stderr = combined
			core.Out().Progress("installing " + target + " ...")
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
	return runSourceStep(dir, verbose, "./install.sh --from-source", script, "--from-source")
}

// runSourceStep runs a single source-upgrade step with optional verbose output.
func runSourceStep(dir string, verbose bool, label string, name string, extraArgs ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), goInstallTimeout)
	defer cancel()
	allArgs := extraArgs
	cmd := exec.CommandContext(ctx, name, allArgs...)
	cmd.Dir = dir
	if verbose {
		cmd.Stdout = core.Out().Err
		cmd.Stderr = core.Out().Err
		core.Out().Progress(label)
	} else {
		combined := new(strings.Builder)
		cmd.Stdout = combined
		cmd.Stderr = combined
		core.Out().Progress(label + " ...")
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

// updateCheckData is the `-o json` payload of rex update -check. Remote is
// null: no remote lookup is wired.
type updateCheckData struct {
	Version string  `json:"version"`
	Remote  *string `json:"remote"`
}

func runUpdateCheck(p *core.Printer) error {
	slog.Info("update: -check mode, printed version", "version", core.Version)
	return p.Emit(core.Result{
		Cmd:  "update",
		Data: updateCheckData{Version: core.Version},
		View: core.View{core.KV{Pairs: []core.Pair{
			{Key: "version", Label: "version", Val: core.Cell{Text: core.Version}},
			{Key: "remote", Label: "remote", Val: core.Cell{Text: "(version check not yet wired — no remote lookup performed)", Raw: ""}},
		}}},
	})
}

// readInstalledVersion runs `rex --version` on the current executable to get
// the post-upgrade version string. Falls back to core.Version constant on error
// (go install replaces the binary, so os.Executable() may now be new).
func readInstalledVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return core.Version
	}
	return versionOf(exe)
}

// versionProbeTimeout bounds `exe --version`; tests on a loaded machine
// raise it.
var versionProbeTimeout = 5 * time.Second

// versionOf runs `exe --version` and returns its one line. The child gets
// plain output: an inherited CLAUDECODE, REX_AGENT or REX_OUTPUT would make
// it print an agent or json line instead of the version.
func versionOf(exe string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--version")
	cmd.Env = append(os.Environ(), "REX_AGENT=0", "REX_OUTPUT=txt")
	out, err := cmd.Output()
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
