package setup

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
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
	lookDaemon  func() string          // the rex-daemon `rex daemon start` runs, or ""
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
	p := core.Out()
	return uninstallDeps{
		out:        p.Out,
		errOut:     p.Err,
		stdin:      p.In,
		home:       paths.Home,
		executable: os.Executable,
		lookDaemon: lookDaemonBinary,
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
	// The result goes to d.out; the plan, the question and warnings to d.errOut.
	p, err := core.Out().WithIO(d.out, d.errOut, d.stdin)
	if err != nil {
		return err
	}
	plan, err := newUninstallPlan(opts, d)
	if err != nil {
		return err
	}

	if opts.dryRun {
		return p.Emit(core.Result{Cmd: "uninstall", Data: plan.data(true, nil), View: plan.planView(true, nil)})
	}
	if err := plan.checkBinaryLocations(); err != nil {
		return err
	}
	if !opts.yes {
		p.EmitErr(plan.planView(false, nil))
		question := "proceed with all removals?"
		if opts.all {
			question = "proceed?"
		}
		ok, err := p.Confirm(question, core.Refusal{Cmd: "uninstall", Flag: "-yes", What: "remove", Extra: "or -dry-run to list",
			Hint: "rex uninstall -yes"})
		if err != nil {
			return err
		}
		if !ok {
			return core.Errorf(core.ExitOperationRefused, "", "uninstall: not confirmed, nothing removed")
		}
	}
	if plan.needsDaemonStopped() {
		if err := stopDaemonForUninstall(p, d, opts.force); err != nil {
			return err
		}
	}
	done, failed := plan.execute(p, d)
	if err := p.Emit(core.Result{Cmd: "uninstall", Data: plan.data(false, done), View: plan.planView(false, done), Failed: failed > 0}); err != nil {
		return err
	}
	if failed > 0 {
		// Each failure was reported above; exit non-zero so a script can
		// tell a partial uninstall from a clean one.
		return core.NewExitError(core.ExitGeneric, "")
	}
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
	out := core.AddOutputFlags(fs, core.AllFormats)
	_, err := out.Parse(fs, args)
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

// removal is one thing a run removes (or, in a plan, would remove).
type removal struct {
	// Kind is binaries, state, sessions, config, profile or daemon.
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// items lists what the plan covers.
func (p *uninstallPlan) items() []removal {
	var out []removal
	o := p.opts
	if o.binaries {
		out = append(out, removal{Kind: "binaries", Path: p.rexBin})
		if p.daemonBin != "" {
			out = append(out, removal{Kind: "binaries", Path: p.daemonBin})
		} else {
			out = append(out, removal{Kind: "binaries", Detail: "rex-daemon (not found on PATH)"})
		}
	}
	if o.wipeState {
		out = append(out, removal{Kind: "state", Path: p.stateDir + "/", Detail: dirSummary(p.stateDir)},
			removal{Kind: "sessions", Path: p.sessionsDir + "/", Detail: dirSummary(p.sessionsDir)})
	}
	if o.purgeConfig {
		out = append(out, removal{Kind: "config", Path: p.configDir + "/"})
	}
	if o.stripProfile {
		found := profileFilesWithBlock(p.profiles)
		for _, f := range found {
			out = append(out, removal{Kind: "profile", Path: f})
		}
		if len(found) == 0 {
			out = append(out, removal{Kind: "profile", Detail: "(no # BEGIN REX blocks found)"})
		}
	}
	return out
}

// uninstallData is the `-o json` payload of rex uninstall.
type uninstallData struct {
	DryRun  bool      `json:"dry_run"`
	Plan    []removal `json:"plan"`
	Removed []removal `json:"removed"`
}

func (p *uninstallPlan) data(dryRun bool, removed []removal) uninstallData {
	if removed == nil {
		removed = []removal{}
	}
	return uninstallData{DryRun: dryRun, Plan: p.items(), Removed: removed}
}

// planView shows the plan (dry run, or before the question) or, once done is
// set, what was removed.
func (p *uninstallPlan) planView(dryRun bool, done []removal) core.View {
	rows := p.items()
	title := "Will remove"
	if done != nil {
		rows, title = done, "Removed"
	}
	cells := make([][]core.Cell, len(rows))
	for i, r := range rows {
		detail := strings.TrimSpace(r.Path + "  " + r.Detail)
		cells[i] = []core.Cell{{Text: r.Kind}, {Text: detail, Raw: r.Path}, {Text: r.Detail}}
	}
	v := core.View{core.Section{Title: title}, core.Table{Kind: "removal", Empty: "(nothing)", Rows: cells, Cols: []core.Col{
		{Key: "kind", Title: "What"},
		{Key: "path", Title: "Where"},
		{Key: "detail", Title: "Detail", Formats: core.SetOf(core.FormatAgent)},
	}}}
	switch {
	case dryRun:
		v = append(v, core.Note{Text: "dry-run: nothing removed"}, agentPairs(core.Field{Key: "dry_run", Val: true}))
	case done != nil:
		v = append(v, core.Note{Text: "run `rm -rf ~/.local/state/rex ~/.config/rex` to be doubly sure"})
	}
	return v
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

// execute performs the removals and returns what was removed. Each failure
// is reported as a warning and the rest still run, so one locked file does
// not block the others.
func (p *uninstallPlan) execute(pr *core.Printer, d uninstallDeps) (done []removal, failed int) {
	done = []removal{}
	if p.opts.binaries {
		for _, path := range []string{p.rexBin, p.daemonBin} {
			if path == "" {
				continue
			}
			if err := removeFile(path); err != nil {
				pr.Warn(fmt.Sprintf("error removing %s: %v", path, err))
				failed++
				continue
			}
			done = append(done, removal{Kind: "binaries", Path: path})
			slog.Info("uninstall: removed binary", "path", path)
		}
	}
	if p.opts.wipeState {
		done, failed = removeDirReported(pr, done, failed, "state", p.stateDir)
		done, failed = removeDirReported(pr, done, failed, "sessions", p.sessionsDir)
	}
	if p.opts.purgeConfig {
		done, failed = removeDirReported(pr, done, failed, "config", p.configDir)
	}
	if p.opts.stripProfile {
		stripped, errs := stripProfileBlocks(p.profiles, d.now())
		for _, e := range errs {
			pr.Warn(fmt.Sprintf("error stripping profile: %v", e))
		}
		failed += len(errs)
		for _, s := range stripped {
			done = append(done, removal{Kind: "profile", Path: s.Path, Detail: "backup: " + s.Backup})
		}
	}
	return done, failed
}

// stopDaemonForUninstall stops the daemon and confirms its socket is gone
// before anything is removed. A daemon that is still answering means
// removals would race its writes, so this refuses (ExitOperationRefused)
// unless force is set, in which case it only warns.
func stopDaemonForUninstall(p *core.Printer, d uninstallDeps, force bool) error {
	wasRunning := d.daemon.running()
	err := d.daemon.stopAndWait()
	if err == nil {
		if wasRunning {
			slog.Info("uninstall: daemon stopped", "socket", d.daemon.socket)
			p.Progress("stopped rex-daemon")
		}
		return nil
	}
	if force {
		p.Warn(fmt.Sprintf("%v; continuing because of -force", err))
		return nil
	}
	return core.NewExitError(core.ExitOperationRefused,
		fmt.Sprintf("%v; nothing was removed. Stop it with `rex daemon stop` and retry, or pass -force", err))
}

// ── helpers ──────────────────────────────────────────────────────────────────

// isUnder reports whether path is dir or inside it. It compares cleaned
// paths element-wise, so /home/alice is not inside /home/al.
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// lookDaemonBinary is the rex-daemon `rex daemon start` would run (next to
// rex, then PATH), or "" when there is none.
func lookDaemonBinary() string {
	if p := findDaemonBinary(); p != "rex-daemon" {
		return p
	}
	return ""
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeDirReported removes dir and appends it to done, or warns and counts
// the failure.
func removeDirReported(p *core.Printer, done []removal, failed int, kind, dir string) ([]removal, int) {
	if err := os.RemoveAll(dir); err != nil {
		p.Warn(fmt.Sprintf("error removing %s: %v", dir, err))
		return done, failed + 1
	}
	slog.Info("uninstall: removed dir", "path", dir)
	return append(done, removal{Kind: kind, Path: dir}), failed
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
