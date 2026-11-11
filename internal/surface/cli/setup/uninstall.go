package setup

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

var (
	uninstallOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50"))
	uninstallBullet = lipgloss.NewStyle().Foreground(lipgloss.Color("#7A7F8C"))
)

// uninstallOpts are the parsed `rex uninstall` flags.
type uninstallOpts struct {
	yes, all, dryRun, force                        bool
	binaries, wipeState, purgeConfig, stripProfile bool
}

// uninstallDeps are RunUninstall's side effects. Tests replace them to run
// the whole flow against a temp HOME and a fake daemon.
type uninstallDeps struct {
	out, errOut io.Writer
	stdin       io.Reader
	home        func() (string, error)
	executable  func() (string, error) // the running rex binary
	lookDaemon  func() string          // rex-daemon on PATH, or ""
	daemon      daemonControl
	now         func() time.Time // names profile backups
}

// defaultUninstallDeps wires RunUninstall to the real system. It fails
// when the daemon socket cannot be resolved (no home directory).
func defaultUninstallDeps() (uninstallDeps, error) {
	daemon, err := defaultDaemonControl()
	if err != nil {
		return uninstallDeps{}, err
	}
	return uninstallDeps{
		out:        os.Stdout,
		errOut:     os.Stderr,
		stdin:      os.Stdin,
		home:       paths.Home,
		executable: os.Executable,
		lookDaemon: func() string { p, _ := exec.LookPath("rex-daemon"); return p },
		daemon:     daemon,
		now:        time.Now,
	}, nil
}

// uninstallPlan is everything one uninstall run may remove.
type uninstallPlan struct {
	opts        uninstallOpts
	home        string
	rexBin      string // symlink-resolved; "" unless opts.binaries
	daemonBin   string // "" when not removing binaries or not on PATH
	stateDir    string
	sessionsDir string
	configDir   string
	profiles    []string
}

// RunUninstall removes rex artifacts after one confirmation.
//
// Order matters: nothing is removed until the daemon is confirmed stopped
// (its socket no longer answers), because a live daemon keeps writing
// session state and would recreate what -wipe-state deletes. If the daemon
// will not stop, uninstall refuses unless -force is given.
func RunUninstall(args []string) error {
	d, err := defaultUninstallDeps()
	if err != nil {
		return err
	}
	return runUninstall(args, d)
}

func runUninstall(args []string, d uninstallDeps) error {
	opts, err := parseUninstallFlags(args, d.errOut)
	if err != nil {
		return err
	}
	plan, err := newUninstallPlan(opts, d)
	if err != nil {
		return err
	}

	plan.preview(d.out)
	if opts.dryRun {
		fmt.Fprintln(d.out)
		fmt.Fprintln(d.out, "  "+uninstallBullet.Render(core.HelpBullet)+" "+core.HelpDim.Render("dry-run: nothing removed"))
		fmt.Fprintln(d.out)
		return nil
	}
	fmt.Fprintln(d.out)

	if err := plan.checkBinaryLocations(); err != nil {
		return err
	}
	if !confirmUninstall(opts, d) {
		return core.NewExitError(core.ExitGeneric, "aborted")
	}
	if plan.needsDaemonStopped() {
		if err := stopDaemonForUninstall(d, opts.force); err != nil {
			return err
		}
	}
	plan.execute(d)

	fmt.Fprintln(d.out)
	fmt.Fprintln(d.out, "  "+uninstallBullet.Render(core.HelpBullet)+" "+core.HelpDim.Render("run `rm -rf ~/.local/state/rex ~/.config/rex` to be doubly sure"))
	fmt.Fprintln(d.out)
	return nil
}

// parseUninstallFlags parses args; -all turns on every removal.
func parseUninstallFlags(args []string, errOut io.Writer) (uninstallOpts, error) {
	var o uninstallOpts
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.BoolVar(&o.yes, "yes", false, "skip confirmations")
	fs.BoolVar(&o.yes, "y", false, "skip confirmations (shorthand)")
	fs.BoolVar(&o.binaries, "binaries", true, "remove rex and rex-daemon binaries")
	fs.BoolVar(&o.wipeState, "wipe-state", false, "remove ~/.local/state/rex/ and ~/.local/share/rex/sessions/")
	fs.BoolVar(&o.purgeConfig, "purge-config", false, "remove ~/.config/rex/")
	fs.BoolVar(&o.stripProfile, "strip-profile", false, "remove # BEGIN REX … # END REX blocks from shell profiles (backups are kept)")
	fs.BoolVar(&o.all, "all", false, "equivalent to all flags above")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print what would be removed without touching anything")
	fs.BoolVar(&o.force, "force", false, "remove files even if rex-daemon is still running after the stop request")
	_, err := core.ParseArgs(fs, args)
	if err != nil {
		return o, err
	}
	if o.all {
		o.binaries, o.wipeState, o.purgeConfig, o.stripProfile = true, true, true, true
	}
	return o, nil
}

// newUninstallPlan resolves every path the run may touch. Binary paths are
// only resolved when binaries are being removed.
func newUninstallPlan(opts uninstallOpts, d uninstallDeps) (*uninstallPlan, error) {
	home, err := d.home()
	if err != nil {
		return nil, core.NewExitError(core.ExitGeneric, "cannot determine home directory: "+err.Error())
	}
	l := paths.ForHome(home)
	p := &uninstallPlan{
		opts:        opts,
		home:        home,
		stateDir:    l.LogDir,
		sessionsDir: l.SessionsDir,
		configDir:   l.ConfigDir,
		profiles:    profileFiles(home),
	}
	if !opts.binaries {
		return p, nil
	}
	rexBin, err := d.executable()
	if err != nil {
		return nil, core.NewExitError(core.ExitGeneric, "cannot resolve rex executable: "+err.Error())
	}
	if resolved, err := filepath.EvalSymlinks(rexBin); err == nil {
		rexBin = resolved
	} else if !os.IsNotExist(err) {
		return nil, core.NewExitError(core.ExitGeneric, "cannot resolve rex symlink: "+err.Error())
	}
	p.rexBin = rexBin
	p.daemonBin = d.lookDaemon()
	return p, nil
}

// preview prints what the run would remove.
func (p *uninstallPlan) preview(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+core.HelpHRStyle.Render(strings.Repeat("─", 44)))
	fmt.Fprintln(w, "  "+core.HelpBold.Render("Will remove:"))
	o := p.opts
	if o.binaries {
		fmt.Fprintln(w, "    "+core.HelpSection.Render("binaries")+"     "+core.HelpCmd.Render(p.rexBin))
		if p.daemonBin != "" {
			fmt.Fprintln(w, "                 "+core.HelpCmd.Render(p.daemonBin))
		} else {
			fmt.Fprintln(w, "                 "+core.HelpDim.Render("rex-daemon  (not found on PATH)"))
		}
	}
	if o.wipeState {
		fmt.Fprintln(w, "    "+core.HelpSection.Render("state")+"        "+core.HelpCmd.Render(p.stateDir+"/")+core.HelpDim.Render("  "+dirSummary(p.stateDir)))
		fmt.Fprintln(w, "    "+core.HelpSection.Render("sessions")+"     "+core.HelpCmd.Render(p.sessionsDir+"/")+core.HelpDim.Render("  "+dirSummary(p.sessionsDir)))
	}
	if o.purgeConfig {
		fmt.Fprintln(w, "    "+core.HelpSection.Render("config")+"       "+core.HelpCmd.Render(p.configDir+"/"))
	}
	if o.stripProfile {
		found := profileFilesWithBlock(p.profiles)
		for _, f := range found {
			fmt.Fprintln(w, "    "+core.HelpSection.Render("profile")+"      "+core.HelpCmd.Render(f))
		}
		if len(found) == 0 {
			fmt.Fprintln(w, "    "+core.HelpSection.Render("profile")+"      "+core.HelpDim.Render("(no # BEGIN REX blocks found)"))
		}
	}
}

// checkBinaryLocations refuses to delete binaries outside the user's home
// (e.g. /usr/local/bin): those need sudo and a deliberate manual step.
func (p *uninstallPlan) checkBinaryLocations() error {
	if !p.opts.binaries {
		return nil
	}
	home := p.home
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	for _, b := range []struct{ name, path string }{{"rex", p.rexBin}, {"rex-daemon", p.daemonBin}} {
		if b.path != "" && !isUnder(b.path, home) && !isUnder(b.path, p.home) {
			return core.NewExitError(core.ExitOperationRefused,
				fmt.Sprintf("%s is installed in a system location; remove manually with `sudo rm %s`", b.name, b.path))
		}
	}
	return nil
}

// needsDaemonStopped reports whether the run removes anything a running
// daemon uses or rewrites. Stripping profile lines alone leaves the daemon
// (and the user's live sessions) running.
func (p *uninstallPlan) needsDaemonStopped() bool {
	return p.opts.binaries || p.opts.wipeState || p.opts.purgeConfig
}

// execute performs the removals. Each failure is reported and the rest
// still run, so one locked file does not block the others.
func (p *uninstallPlan) execute(d uninstallDeps) {
	if p.opts.binaries {
		p.removeBinaries(d)
	}
	if p.opts.wipeState {
		removeDirReported(d, p.stateDir, "state removed")
		removeDirReported(d, p.sessionsDir, "sessions removed")
	}
	if p.opts.purgeConfig {
		removeDirReported(d, p.configDir, "config removed")
	}
	if p.opts.stripProfile {
		p.stripProfiles(d)
	}
}

func (p *uninstallPlan) removeBinaries(d uninstallDeps) {
	removed := false
	for _, path := range []string{p.rexBin, p.daemonBin} {
		if path == "" {
			continue
		}
		if err := removeFile(path); err != nil {
			fmt.Fprintf(d.errOut, "  error removing %s: %v\n", path, err)
			continue
		}
		removed = true
		slog.Info("uninstall: removed binary", "path", path)
	}
	if removed {
		fmt.Fprintln(d.out, "  "+uninstallOK.Render("✓")+" "+core.HelpCmd.Render("binaries removed"))
	}
}

func (p *uninstallPlan) stripProfiles(d uninstallDeps) {
	stripped, errs := stripProfileBlocks(p.profiles, d.now())
	for _, e := range errs {
		fmt.Fprintf(d.errOut, "  error stripping profile: %v\n", e)
	}
	for _, s := range stripped {
		fmt.Fprintln(d.out, "  "+uninstallOK.Render("✓")+" "+core.HelpCmd.Render("profile block removed from "+s.Path)+
			core.HelpDim.Render("  (backup: "+s.Backup+")"))
	}
	if len(stripped) == 0 && len(errs) == 0 {
		fmt.Fprintln(d.out, "  "+uninstallBullet.Render(core.HelpBullet)+" "+core.HelpDim.Render("no profile blocks found"))
	}
}

// confirmUninstall asks once unless -yes was given.
func confirmUninstall(o uninstallOpts, d uninstallDeps) bool {
	switch {
	case o.yes:
		return true
	case o.all:
		return confirm(d, "  proceed? [y/N] ")
	default:
		return confirm(d, "  proceed with all removals? [y/N] ")
	}
}

// stopDaemonForUninstall stops the daemon and confirms its socket is gone
// before anything is removed. A daemon that is still answering means
// removals would race its writes, so this refuses (ExitOperationRefused)
// unless force is set, in which case it only warns.
func stopDaemonForUninstall(d uninstallDeps, force bool) error {
	wasRunning := d.daemon.running()
	err := d.daemon.stopAndWait()
	if err == nil {
		if wasRunning {
			slog.Info("uninstall: daemon stopped", "socket", d.daemon.socket)
			fmt.Fprintln(d.out, "  "+uninstallBullet.Render(core.HelpBullet)+" "+core.HelpDim.Render("stopped rex-daemon"))
		}
		return nil
	}
	if force {
		fmt.Fprintf(d.errOut, "  warning: %v; continuing because of -force\n", err)
		return nil
	}
	return core.NewExitError(core.ExitOperationRefused,
		fmt.Sprintf("%v; nothing was removed. Stop it with `rex daemon stop` and retry, or pass -force", err))
}

// ── helpers ──────────────────────────────────────────────────────────────────

func confirm(d uninstallDeps, prompt string) bool {
	fmt.Fprint(d.out, prompt)
	var ans string
	_, _ = fmt.Fscanln(d.stdin, &ans)
	return ans == "y" || ans == "Y" || ans == "yes"
}

// isUnder reports whether path is dir or inside it. It compares cleaned
// paths element-wise, so /home/alice is not inside /home/al.
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeDirReported removes dir and prints done on success or the error.
func removeDirReported(d uninstallDeps, dir, done string) {
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(d.errOut, "  error removing %s: %v\n", dir, err)
		return
	}
	fmt.Fprintln(d.out, "  "+uninstallOK.Render("✓")+" "+core.HelpCmd.Render(done))
	slog.Info("uninstall: removed dir", "path", dir)
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
