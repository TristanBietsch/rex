package setup

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/selfupdate"
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
	// release is where release builds come from (GitHub by default).
	release releaseSource
	// version is the running rex's version (core.Version).
	version      string
	goos, goarch string
	getenv       func(string) string
	tempDir      string // "" is the system temp dir
	hasUpgrade   func(exe string) bool
	checkTimeout time.Duration
	fetchTimeout time.Duration
}

// releaseSource looks up and downloads release builds.
type releaseSource interface {
	Latest(ctx context.Context) (string, error)
	Fetch(ctx context.Context, tag, goos, goarch, tmpDir string) (*selfupdate.Archive, error)
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
		release:          &selfupdate.Source{Token: os.Getenv("GITHUB_TOKEN")},
		version:          core.Version,
		goos:             runtime.GOOS,
		goarch:           runtime.GOARCH,
		getenv:           os.Getenv,
		hasUpgrade:       hasUpgradeCommand,
	}, nil
}

// updateFlags are rex update's options.
type updateFlags struct {
	check, yes, force, verbose bool
	source, version            string
}

// RunUpdate upgrades the rex and rex-daemon binaries in place.
//
// Ordering invariant: everything that can refuse (install-mode detection,
// the version checks, the confirmation prompt) runs before anything is
// stopped or written, so a refused or cancelled update changes nothing.
// A release install (install.sh, install.ps1, a release archive) is
// updated from GitHub releases and its daemon is left running. A go install
// or -source upgrade stops the daemon right before the upgrade.
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
	var o updateFlags
	fs.BoolVar(&o.check, "check", false, "look up the latest release and compare; changes nothing")
	fs.BoolVar(&o.yes, "yes", false, "skip confirmation prompt")
	yShort := fs.Bool("y", false, "skip confirmation prompt (shorthand)")
	fs.BoolVar(&o.force, "force", false, "install even when the release is not newer, or the running version is unknown")
	fs.StringVar(&o.version, "version", "", "install this release (vX.Y.Z) instead of the latest; also REX_VERSION")
	fs.StringVar(&o.source, "source", "", "explicit source checkout path; run git pull + ./install.sh --from-source")
	fs.BoolVar(&o.verbose, "verbose", false, "stream go install output instead of spinning")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	o.yes = o.yes || *yShort
	if o.version == "" && d.getenv != nil {
		o.version = d.getenv("REX_VERSION")
	}
	// The result goes to d.out, progress, the plan and the question to d.errOut.
	p, err := core.Out().WithIO(d.out, d.errOut, d.stdin)
	if err != nil {
		return err
	}

	slog.Info("update: starting", "check", o.check, "source", o.source, "version", o.version)

	if o.check {
		return runUpdateCheck(p, d, o)
	}

	mode, exePath, err := d.detectMode()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: could not determine install mode: %v", err))
	}
	if o.source == "" {
		switch mode {
		case installModePackage:
			return refusePackageManager(exePath)
		case installModeRelease:
			if repo := guessSourceDir(exePath); repo != "" {
				return refuseSourceCheckout(exePath, repo)
			}
			return runReleaseUpdate(p, d, o, exePath)
		case installModeGoInstall:
			// go install below.
		}
	}

	if o.source == "" && o.version != "" {
		// go install always builds @latest (the module path has no /v2,
		// so Go ignores release tags); refuse rather than ignore the pin.
		return core.Errorf(core.ExitInvalidArgs, "rex update",
			"update: -version and REX_VERSION apply to release installs; a go install build updates to the latest master")
	}

	daemonRunning := d.daemon.running()
	printUpdatePlan(p, exePath, o.source, mode, daemonRunning)

	if !o.yes {
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

	if err := runUpgrade(d, o.source, o.verbose); err != nil {
		return core.NewExitError(core.ExitGeneric, fmt.Sprintf("update: %v", err))
	}

	newVer := d.installedVersion()
	slog.Info("update: complete", "old_version", d.version, "new_version", newVer)
	v := core.ActionView(core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: fmt.Sprintf("rex %s → %s", d.version, newVer)},
		core.Field{Key: "from", Val: d.version}, core.Field{Key: "to", Val: newVer},
		core.Field{Key: "daemon_restart", Val: daemonRunning})
	if daemonRunning {
		v = append(v, core.Note{Text: "daemon will restart on next `rex`"})
	}
	return p.Emit(core.Result{Cmd: "update", Data: updateData{From: d.version, To: newVer, DaemonRestart: daemonRunning}, View: v})
}

// updateData is the `-o json` payload of rex update.
type updateData struct {
	From          string `json:"from"`
	To            string `json:"to"`
	DaemonRestart bool   `json:"daemon_restart"`
	// Next is the command that moves a running daemon to the new binary
	// (release updates only; empty when no daemon runs).
	Next string `json:"next,omitempty"`
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

// installerHint is the command that installs the latest release with
// install.sh.
const installerHint = "curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh && sh install.sh"

// refuseSourceCheckout refuses to overwrite a binary that sits in a source
// checkout (built there, not installed): rebuilding is the upgrade.
func refuseSourceCheckout(exePath, repo string) error {
	slog.Info("update: binary in a source checkout, not updating from releases", "exe", exePath)
	return core.Errorf(core.ExitOperationRefused, fmt.Sprintf("rex update -source %s", repo),
		"rex at %s was built in a source checkout; rebuild it there (or use -source)", exePath)
}

// refusePackageManager refuses to overwrite a binary a package manager owns.
func refusePackageManager(exePath string) error {
	mgr := packageManagerOf(exePath)
	slog.Info("update: package-manager install, not updating", "exe", exePath, "manager", mgr)
	hint := ""
	switch mgr {
	case "Homebrew":
		hint = "brew upgrade rex"
	case "Scoop":
		hint = "scoop update rex"
	}
	return core.Errorf(core.ExitOperationRefused, hint,
		"rex at %s is managed by %s; upgrade it there", exePath, mgr)
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
	installModeGoInstall installMode = iota
	// installModeRelease is a release build in a folder rex owns
	// (install.sh's ~/.local/bin, install.ps1, an unpacked archive).
	installModeRelease
	// installModePackage is a binary a package manager owns (Homebrew,
	// Scoop, Nix, a distro package): rex never overwrites it.
	installModePackage
)

// detectInstallMode classifies the running rex binary and returns its
// symlink-resolved path.
func detectInstallMode() (installMode, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, "", err
	}
	// Resolve symlinks so we compare real paths (a Homebrew bin link
	// resolves into the Cellar).
	// An unresolved link could hide a package manager's file, so a
	// failure here refuses rather than guessing.
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return 0, "", fmt.Errorf("resolving %s: %w", exe, err)
	}
	gopath := os.Getenv("GOPATH")
	home, err := paths.Home()
	if err != nil && gopath == "" {
		// ~/go/bin cannot be located; a relative "go/bin" would match
		// nothing meaningful.
		return 0, "", err
	}
	return classifyInstall(real, home, gopath, os.Getenv("GOBIN")), real, nil
}

// classifyInstall reports go-install mode when exe is under $GOPATH/bin
// (default ~/go/bin) or ~/go/bin, package mode when a package manager owns
// it, and release mode otherwise.
func classifyInstall(exe, home, gopath, gobin string) installMode {
	if gopath == "" {
		gopath = filepath.Join(home, "go")
	}
	dirs := []string{filepath.Join(gopath, "bin"), filepath.Join(home, "go", "bin")}
	if gobin != "" {
		dirs = append(dirs, gobin)
	}
	for _, binDir := range dirs {
		if hasPathPrefix(exe, binDir) {
			return installModeGoInstall
		}
	}
	if packageManagerOf(exe) != "" {
		return installModePackage
	}
	return installModeRelease
}

// hasPathPrefix reports whether p is inside dir. Windows paths compare
// case-insensitively.
func hasPathPrefix(p, dir string) bool {
	prefix := filepath.Clean(dir) + string(os.PathSeparator)
	if runtime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(p), strings.ToLower(prefix))
	}
	return strings.HasPrefix(p, prefix)
}

// packageManagers maps a path fragment (slash-separated, lower case) to the
// package manager whose files live there.
var packageManagers = []struct{ frag, name string }{
	{"/cellar/", "Homebrew"},
	{"/homebrew/", "Homebrew"},
	{"/linuxbrew/", "Homebrew"},
	{"/nix/store/", "Nix"},
	{"/snap/", "Snap"},
	{"/flatpak/", "Flatpak"},
	{"/opt/local/", "MacPorts"},
	{"/scoop/", "Scoop"},
	{"/chocolatey/", "Chocolatey"},
	{"/winget/", "WinGet"},
}

// packageManagerOf names the package manager that owns exe, or "" when
// none does. /usr/bin and /bin hold distro packages.
func packageManagerOf(exe string) string {
	p := strings.ToLower(strings.ReplaceAll(exe, "\\", "/"))
	for _, m := range packageManagers {
		if strings.Contains(p, m.frag) {
			return m.name
		}
	}
	switch path.Dir(p) {
	case "/usr/bin", "/usr/sbin", "/bin", "/sbin":
		return "the system package manager"
	}
	return ""
}

func modeLabel(m installMode) string {
	switch m {
	case installModeGoInstall:
		return "go install"
	case installModePackage:
		return "package manager"
	default:
		return "release build"
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
		return runSourceStep(dir, verbose, "install.ps1", "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(dir, "install.ps1"), "-FromSource")
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
