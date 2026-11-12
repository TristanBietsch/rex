package lifecycle

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// boardSwapWait is how long rex upgrade waits for boards to come back on
// the new build.
const boardSwapWait = 10 * time.Second

// upgradeProc is one process in the rex upgrade report.
type upgradeProc struct {
	Kind   string `json:"kind"` // daemon or board
	PID    int    `json:"pid,omitempty"`
	Result string `json:"result"` // hot_restarted, cold_restarted, swapped, old_build, gone, not_running
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
}

// RunUpgrade is `rex upgrade [-cold] [-yes]`: after a new build is
// installed, hot-restart the daemon, then have every board swap into the
// new binary, and report each process.
func RunUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	cold := fs.Bool("cold", false, "restart the daemon cold (kills running sessions)")
	yes := fs.Bool("yes", false, "do not ask before a cold restart kills running sessions")
	fs.BoolVar(yes, "y", false, "same as -yes")
	if _, err := out.Parse(fs, args); err != nil {
		return err
	}
	c, err := defaultDaemonCtl()
	if err != nil {
		return err
	}
	c.cold, c.yes = *cold, *yes
	return upgrader{ctl: c, boards: daemonctl.Boards, swap: daemonctl.SwapBoards, wait: boardSwapWait, poll: 250 * time.Millisecond}.run()
}

func (u upgrader) run() error {
	c := u.ctl
	p := printerFor(c.out, c.errOut)
	ctx := context.Background()
	var procs []upgradeProc
	if !c.reachable(c.socket) {
		return c.notRunning()
	}
	// The daemon: a hot restart, or (refused, -cold) the restart command's
	// cold path with its confirmation; its own lines go to stderr.
	var build string
	daemon := upgradeProc{Kind: "daemon"}
	var res daemonctl.HotResult
	var err error
	hotDone := false
	if !c.cold && c.hot != nil {
		res, err = c.hot(ctx, c.socket)
		switch {
		case err == nil:
			hotDone = true
		case errors.Is(err, daemonctl.ErrHotUnavailable):
			p.Warn(err.Error() + "; falling back to a cold restart")
		default:
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
	}
	if hotDone {
		kept := res.After.Live
		daemon.PID, daemon.Result, daemon.From, daemon.Build, daemon.Kept = res.After.PID, "hot_restarted", res.Before.Build, res.After.Build, &kept
		build = res.After.Build
	} else {
		cold := c
		cold.cold = true
		cold.out = io.Discard
		if err := cold.restart(); err != nil {
			return err
		}
		daemon.Result = "cold_restarted"
		if c.info != nil {
			if d, err := c.info(ctx, c.socket); err == nil {
				daemon.PID, daemon.Build = d.PID, d.Build
				build = d.Build
			}
		}
	}
	procs = append(procs, daemon)

	// The boards: those on another build swap; wait for them to come back.
	asked, err := u.swap(ctx, c.socket, build)
	if err != nil {
		p.Warn("could not ask the boards to swap: " + err.Error())
	}
	pending := map[int]protocol.BoardInfo{}
	for _, b := range asked {
		pending[b.PID] = b
	}
	results := map[int]string{}
	deadline := time.Now().Add(u.wait)
	for len(results) < len(pending) && time.Now().Before(deadline) {
		time.Sleep(u.poll)
		now, err := u.boards(ctx, c.socket)
		if err != nil {
			continue
		}
		for _, b := range now {
			if _, ok := pending[b.PID]; ok && b.PID != 0 && b.Build == build {
				results[b.PID] = "swapped"
			}
		}
	}
	for _, b := range asked {
		pid := b.PID
		r := results[pid]
		if r == "" {
			r = "old_build"
		}
		procs = append(procs, upgradeProc{Kind: "board", PID: pid, Result: r, From: b.Build, Build: build})
	}
	return emitUpgrade(p, procs)
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
