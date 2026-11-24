package lifecycle

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/version"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// boardSwapWait is how long rex upgrade waits for boards to come back on
// the new build.
const boardSwapWait = 10 * time.Second

// upgradeProc is one process in the rex upgrade report.
type upgradeProc struct {
	Kind   string `json:"kind"` // daemon or board
	PID    int    `json:"pid,omitempty"`
	Result string `json:"result"` // hot_restarted, cold_restarted, current, swapped, old_build, gone, not_running
	From   string `json:"from_build,omitempty"`
	Build  string `json:"build,omitempty"`
	Kept   *int   `json:"kept,omitempty"`
}

// upgradeData is the -o json payload of rex upgrade.
type upgradeData struct {
	Processes []upgradeProc `json:"processes"`
}

// upgrader is rex upgrade; its fields are daemonctl functions in
// production and fakes in tests.
type upgrader struct {
	ctl    daemonCtl
	boards func(ctx context.Context, socket string) ([]protocol.BoardInfo, error)
	swap   func(ctx context.Context, socket, build string) ([]protocol.BoardInfo, error)
	wait   time.Duration
	poll   time.Duration
	// self is the build of this rex: the one boards swap to.
	self string
	// installed is the build of the rex-daemon binary rex would start
	// ("" when unknown); nil in tests that do not care.
	installed func() string
	// ifStale skips a daemon already on the installed build; noCold
	// refuses instead of falling back to a cold restart.
	ifStale, noCold bool
}

// RunUpgrade is `rex upgrade [-cold] [-yes] [-if-stale] [-no-cold]`: after
// a new build is installed, hot-restart the daemon, then have every board
// swap into the new binary, and report each process.
func RunUpgrade(args []string) error {
	// Hash this binary before anything else, so the build is the code that
	// is running even if the file is replaced while we work.
	self := version.Build()
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	cold := fs.Bool("cold", false, "restart the daemon cold (kills running sessions)")
	yes := fs.Bool("yes", false, "do not ask before a cold restart kills running sessions")
	fs.BoolVar(yes, "y", false, "same as -yes")
	ifStale := fs.Bool("if-stale", false, "leave the daemon alone when it already runs the installed build")
	noCold := fs.Bool("no-cold", false, "never cold-restart: refuse when a hot restart is not possible")
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	if *cold && *noCold {
		return core.Errorf(core.ExitInvalidArgs, "rex upgrade -no-cold", "upgrade: -cold and -no-cold contradict each other")
	}
	c, err := defaultDaemonCtl()
	if err != nil {
		return err
	}
	c.cold, c.yes = *cold, *yes
	return upgrader{
		ctl: c, boards: daemonctl.Boards, swap: daemonctl.SwapBoards, wait: boardSwapWait, poll: 250 * time.Millisecond,
		self:      self,
		installed: func() string { return version.FileBuild(daemonctl.FindBinary()) },
		ifStale:   *ifStale, noCold: *noCold,
	}.run()
}

func (u upgrader) run() error {
	c := u.ctl
	p := printerFor(c.out, c.errOut)
	ctx := context.Background()
	if !c.reachable(c.socket) {
		return c.notRunning()
	}
	installed := ""
	if u.installed != nil {
		installed = u.installed()
	}
	// A hot restart drops every board; list them first so the swap can
	// wait for them to come back.
	var before []protocol.BoardInfo
	if u.boards != nil {
		before, _ = u.boards(ctx, c.socket)
	}
	slog.Info("upgrade: starting", "self", u.self, "installed", installed, "boards", len(before),
		"if_stale", u.ifStale, "no_cold", u.noCold)

	daemon, restarted, err := u.daemon(p, installed)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(u.wait)
	if restarted {
		u.awaitBoards(ctx, before, deadline)
	}

	// The boards: those on another build than this rex swap; wait for them
	// to come back on it.
	asked, err := u.swap(ctx, c.socket, u.self)
	if err != nil {
		p.Warn("could not ask the boards to swap: " + err.Error())
	}
	if !restarted && len(asked) == 0 {
		slog.Info("upgrade: nothing to do", "build", daemon.Build, "boards", len(before))
		return emitCurrent(p, daemon, len(before), u.self)
	}
	procs := []upgradeProc{daemon}
	procs = append(procs, u.awaitSwaps(ctx, asked, deadline)...)
	if err := emitUpgrade(p, procs); err != nil {
		return err
	}
	// The daemon re-executes the path it was started from, which need not
	// be the binary that was just installed.
	if restarted && installed != "" && daemon.Build != "" && daemon.Build != installed {
		slog.Warn("upgrade: daemon not on the installed build", "build", daemon.Build, "installed", installed)
		return core.Errorf(core.ExitGeneric, "rex daemon restart -cold",
			"upgrade: the daemon is on build %s but the installed rex-daemon (%s) is build %s; it re-executes the path it was started from",
			daemon.Build, daemonctl.FindBinary(), installed)
	}
	return nil
}

// daemon moves the daemon onto the installed build: nothing when -if-stale
// finds it current, else a hot restart, else (unless -no-cold) the restart
// command's cold path with its confirmation. restarted is false only when
// the daemon was left alone.
func (u upgrader) daemon(p *core.Printer, installed string) (upgradeProc, bool, error) {
	c := u.ctl
	ctx := context.Background()
	proc := upgradeProc{Kind: "daemon"}
	if u.ifStale && installed != "" && c.info != nil {
		if d, err := c.info(ctx, c.socket); err == nil && d.Build == installed {
			proc.PID, proc.Result, proc.Build = d.PID, "current", d.Build
			return proc, false, nil
		}
	}
	if !c.cold && c.hot != nil {
		res, err := c.hot(ctx, c.socket)
		switch {
		case err == nil:
			kept := res.After.Live
			proc.PID, proc.Result, proc.From, proc.Build, proc.Kept = res.After.PID, "hot_restarted", res.Before.Build, res.After.Build, &kept
			return proc, true, nil
		case !errors.Is(err, daemonctl.ErrHotUnavailable):
			// The old image may already be gone: not "nothing restarted".
			return proc, false, core.NewExitError(core.ExitGeneric, err.Error())
		case u.noCold:
			slog.Info("upgrade: hot restart refused, no cold fallback", "err", err)
			return proc, false, core.Errorf(core.ExitOperationRefused, "rex upgrade",
				"upgrade: %v; nothing was restarted", err)
		}
		p.Warn(err.Error() + "; falling back to a cold restart")
	} else if u.noCold {
		return proc, false, core.Errorf(core.ExitOperationRefused, "rex upgrade",
			"upgrade: hot restart is not supported on this platform; nothing was restarted")
	}
	cold := c
	cold.cold = true
	cold.out = io.Discard
	if err := cold.restart(); err != nil {
		return proc, false, err
	}
	proc.Result = "cold_restarted"
	if c.info != nil {
		if d, err := c.info(ctx, c.socket); err == nil {
			proc.PID, proc.Build = d.PID, d.Build
		}
	}
	return proc, true, nil
}

// awaitBoards waits until every board listed before the restart has
// reconnected (they redial with a backoff), or until deadline.
func (u upgrader) awaitBoards(ctx context.Context, before []protocol.BoardInfo, deadline time.Time) {
	want := map[int]bool{}
	for _, b := range before {
		if b.PID != 0 {
			want[b.PID] = true
		}
	}
	for len(want) > 0 && time.Now().Before(deadline) {
		time.Sleep(u.poll)
		now, err := u.boards(ctx, u.ctl.socket)
		if err != nil {
			continue
		}
		for _, b := range now {
			delete(want, b.PID)
		}
	}
	if len(want) > 0 {
		slog.Info("upgrade: boards not back before the swap", "missing", len(want))
	}
}

// awaitSwaps waits until each asked board is back on this rex's build, or
// until deadline, and reports each one.
func (u upgrader) awaitSwaps(ctx context.Context, asked []protocol.BoardInfo, deadline time.Time) []upgradeProc {
	pending := map[int]protocol.BoardInfo{}
	for _, b := range asked {
		pending[b.PID] = b
	}
	results := map[int]string{}
	for len(results) < len(pending) && time.Now().Before(deadline) {
		time.Sleep(u.poll)
		now, err := u.boards(ctx, u.ctl.socket)
		if err != nil {
			continue
		}
		for _, b := range now {
			if _, ok := pending[b.PID]; ok && b.PID != 0 && b.Build == u.self {
				results[b.PID] = "swapped"
			}
		}
	}
	var procs []upgradeProc
	for _, b := range asked {
		r := results[b.PID]
		if r == "" {
			r = "old_build"
		}
		procs = append(procs, upgradeProc{Kind: "board", PID: b.PID, Result: r, From: b.Build, Build: u.self})
	}
	return procs
}

// emitCurrent reports an upgrade with nothing to do: the daemon is on the
// installed build and every board on this rex's.
func emitCurrent(p *core.Printer, daemon upgradeProc, boards int, build string) error {
	text := fmt.Sprintf("nothing to do: daemon on build %s, no board connected", daemon.Build)
	if boards > 0 {
		text = fmt.Sprintf("nothing to do: daemon on build %s, %d board(s) on build %s", daemon.Build, boards, build)
	}
	return p.Emit(core.Result{Cmd: "upgrade", Data: upgradeData{Processes: []upgradeProc{daemon}},
		View: core.View{core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: text}}})
}

func emitUpgrade(p *core.Printer, procs []upgradeProc) error {
	var view core.View
	for _, pr := range procs {
		pid := "pid ?"
		if pr.PID > 0 {
			pid = "pid " + strconv.Itoa(pr.PID)
		}
		text := ""
		tone, glyph := core.ToneOK, p.Glyphs.OK
		switch pr.Result {
		case "hot_restarted":
			text = fmt.Sprintf("daemon %s: hot-restarted, %d session(s) kept (build %s → %s)", pid, *pr.Kept, pr.From, pr.Build)
		case "cold_restarted":
			text = fmt.Sprintf("daemon %s: cold-restarted (build %s)", pid, pr.Build)
		case "current":
			text = fmt.Sprintf("daemon %s: already on build %s", pid, pr.Build)
		case "swapped":
			text = fmt.Sprintf("board %s: swapped to build %s", pid, pr.Build)
		default:
			text = fmt.Sprintf("board %s: still on build %s (it swaps when it is idle, or press : swap)", pid, pr.From)
			tone, glyph = core.ToneWarn, p.Glyphs.Warn
		}
		view = append(view, core.Line{Tone: tone, Glyph: glyph, Text: text})
	}
	if len(procs) == 1 {
		view = append(view, core.Line{Text: "no board was connected"})
	}
	return p.Emit(core.Result{Cmd: "upgrade", Data: upgradeData{Processes: procs}, View: view})
}
