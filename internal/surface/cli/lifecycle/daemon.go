package lifecycle

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunDaemon dispatches: rex daemon start | stop | status | restart | logs
func RunDaemon(args []string) error {
	if len(args) > 0 && core.IsHelpArg(args[0]) {
		return core.RunGroupHelp("daemon", args[1:])
	}
	if len(args) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "daemon: subcommand required (start|stop|status|restart|logs)")
	}
	switch args[0] {
	case "start":
		return daemonStart(args[1:])
	case "stop", "status", "restart":
		if _, err := parseDaemonFlags(args[0], args[1:]); err != nil {
			return err
		}
		c, err := defaultDaemonCtl()
		if err != nil {
			return err
		}
		switch args[0] {
		case "stop":
			return c.stop()
		case "status":
			return c.status()
		default:
			return c.restart()
		}
	case "logs":
		return daemonLogs(args[1:])
	default:
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("daemon: unknown subcommand %q", args[0]))
	}
}

// parseDaemonFlags parses a daemon subcommand's output flags and returns its
// positional arguments.
func parseDaemonFlags(sub string, args []string) ([]string, error) {
	fs := flag.NewFlagSet("daemon "+sub, flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.AllFormats)
	return out.Parse(fs, args)
}

// daemonData is the `-o json` payload of the daemon subcommands.
type daemonData struct {
	// Action is start, stop, status or restart.
	Action string `json:"action"`
	// State is started, already_running, stopped, running or not_running.
	State  string `json:"state"`
	Socket string `json:"socket"`
	PID    string `json:"pid,omitempty"`
}

// printerFor is the process Printer writing to out and errOut (the commands
// keep their writers injectable for tests).
func printerFor(out, errOut io.Writer) *core.Printer {
	p, err := core.Out().WithIO(out, errOut, nil)
	if err != nil {
		return core.Out()
	}
	return p
}

// emitDaemon reports a daemon action: a ✓ line in text, the line the command
// always printed in txt, the data in json and one ok line in agent mode.
func emitDaemon(p *core.Printer, d daemonData, text, txt string, failed bool) error {
	fields := []core.Field{{Key: "action", Val: d.Action}, {Key: "state", Val: d.State}, {Key: "socket", Val: d.Socket}}
	if d.PID != "" {
		fields = append(fields, core.Field{Key: "pid", Val: d.PID})
	}
	line := core.Line{Tone: core.ToneOK, Glyph: p.Glyphs.OK, Text: text, Txt: txt}
	if failed {
		line = core.Line{Text: text, Txt: txt}
	}
	return p.Emit(core.Result{Cmd: "daemon", Data: d, View: core.ActionView(line, fields...), Failed: failed})
}

func daemonStart(args []string) error {
	if _, err := parseDaemonFlags("start", args); err != nil {
		return err
	}
	s, err := defaultDaemonStarter()
	if err != nil {
		return err
	}
	return s.run()
}

// defaultDaemonStarter wires daemonStarter to the real daemon on the
// default socket, logging to paths.DaemonLog. It fails when either path
// cannot be resolved (no home directory).
func defaultDaemonStarter() (daemonStarter, error) {
	socket, err := core.DefaultSocket()
	if err != nil {
		return daemonStarter{}, err
	}
	logPath, err := paths.DaemonLog()
	if err != nil {
		return daemonStarter{}, core.NewExitError(core.ExitGeneric, err.Error())
	}
	return daemonStarter{
		socket:    socket,
		logPath:   logPath,
		reachable: daemonctl.Reachable,
		start:     daemonctl.Start,
		out:       core.Out().Out,
		errOut:    core.Out().Err,
	}, nil
}

// daemonStarter is `rex daemon start`. reachable and start are
// daemonctl.Reachable and daemonctl.Start in production; tests swap them so
// no real daemon is spawned.
type daemonStarter struct {
	socket    string
	logPath   string
	reachable func(socket string) bool
	start     func(socket string, stderrLog *os.File) (*daemonctl.StartResult, error)
	out       io.Writer
	errOut    io.Writer
}

// run starts the daemon unless one already answers on the socket. The
// daemon's stderr goes to logPath; failing to open it is an error rather
// than a silently discarded crash log.
func (s daemonStarter) run() error {
	p := printerFor(s.out, s.errOut)
	if s.reachable(s.socket) {
		return emitDaemon(p, daemonData{Action: "start", State: "already_running", Socket: s.socket},
			"rex-daemon already running", "rex-daemon already running", false)
	}
	logf, err := openDaemonLog(s.logPath)
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	// The child inherits its own descriptor; ours is only needed for the spawn.
	defer logf.Close()
	res, err := s.start(s.socket, logf)
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	msg := fmt.Sprintf("rex-daemon started (pid %d)", res.PID)
	return emitDaemon(p, daemonData{Action: "start", State: "started", Socket: s.socket, PID: strconv.Itoa(res.PID)}, msg, msg, false)
}

// openDaemonLog opens the rex-daemon stderr log for appending through
// rexlog's private-open helpers: the directory is created or tightened to
// 0700 and the file to 0600, fixing a log an older version left
// world-readable. Panics and stderr output can carry session paths and
// agent output (backlog S-01).
func openDaemonLog(path string) (*os.File, error) {
	if err := rexlog.PrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("daemon log dir: %w", err)
	}
	f, err := rexlog.OpenPrivate(path)
	if err != nil {
		return nil, fmt.Errorf("daemon log: %w", err)
	}
	return f, nil
}

// daemonCtl is `rex daemon stop`, `status` and `restart`.
//
// The socket is the authority: whatever answers on it is the daemon these
// commands act on, and stopping goes through a Shutdown intent on it. No
// process is looked up by name and signaled. findPID (daemonctl.FindPID)
// is for display only; it fails when the user runs two daemons, which
// must not stop anything from working.
//
// The fields are daemonctl functions in production; tests swap them so no
// real daemon is ever reached.
type daemonCtl struct {
	socket      string
	reachable   func(socket string) bool
	shutdown    func(ctx context.Context, socket string) error
	findPID     func() (string, error)
	starter     daemonStarter // restart's start half
	out, errOut io.Writer
}

// defaultDaemonCtl targets the daemon on the default socket. It fails when
// the socket or daemon log cannot be resolved (no home directory).
func defaultDaemonCtl() (daemonCtl, error) {
	starter, err := defaultDaemonStarter()
	if err != nil {
		return daemonCtl{}, err
	}
	return daemonCtl{
		socket:    starter.socket,
		reachable: daemonctl.Reachable,
		shutdown:  daemonctl.ShutdownDaemon,
		findPID:   daemonctl.FindPID,
		starter:   starter,
		out:       core.Out().Out,
		errOut:    core.Out().Err,
	}, nil
}

// pid returns the daemon's PID for display, or "unknown" when the lookup
// fails (no match, several daemons, pgrep missing).
func (c daemonCtl) pid() string {
	pid, err := c.findPID()
	if err != nil || pid == "" {
		return "unknown"
	}
	return pid
}

// notRunning is the error for a socket nothing answers on.
func (c daemonCtl) notRunning() error {
	return core.NewExitError(core.ExitDaemonUnreachable,
		fmt.Sprintf("%v: nothing answers on %s", daemonctl.ErrNotRunning, c.socket))
}

// stop asks the daemon on the socket to shut down and returns once it has
// removed its socket (daemonctl.ShutdownDaemon waits for that), so a new
// daemon can bind the path straight away.
func (c daemonCtl) stop() error {
	if !c.reachable(c.socket) {
		return c.notRunning()
	}
	// Look the PID up first: once the daemon has exited there is none.
	pid := c.pid()
	if err := c.shutdown(context.Background(), c.socket); err != nil {
		if errors.Is(err, daemonctl.ErrNotRunning) {
			return c.notRunning()
		}
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return emitDaemon(printerFor(c.out, c.errOut), daemonData{Action: "stop", State: "stopped", Socket: c.socket, PID: pid},
		fmt.Sprintf("stopped rex-daemon (pid %s)", pid), fmt.Sprintf("stopped rex-daemon (pid %s) · socket=%s", pid, c.socket), false)
}

// status reports whether the daemon answers on the socket. That is the
// truth: a rex-daemon process that does not answer here (another socket,
// another HOME) does not count, and a failed PID lookup only shows as
// pid=unknown.
func (c daemonCtl) status() error {
	p := printerFor(c.out, c.errOut)
	if !c.reachable(c.socket) {
		txt := fmt.Sprintf("not running · socket=%s", c.socket)
		if err := emitDaemon(p, daemonData{Action: "status", State: "not_running", Socket: c.socket},
			"rex-daemon is not running", txt, true); err != nil {
			return err
		}
		return core.NewExitError(core.ExitDaemonUnreachable, "")
	}
	pid := c.pid()
	return emitDaemon(p, daemonData{Action: "status", State: "running", Socket: c.socket, PID: pid},
		fmt.Sprintf("rex-daemon is running (pid %s)", pid), fmt.Sprintf("running · socket=%s · pid=%s", c.socket, pid), false)
}

// restart stops the daemon if one answers and starts a new one. stop
// returns only once the old daemon has removed its socket, so no settling
// delay is needed. A daemon that will not stop is reported and no second
// daemon is started beside it.
func (c daemonCtl) restart() error {
	if err := c.stop(); err != nil {
		var ec core.ExitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != core.ExitDaemonUnreachable {
			return err
		}
		printerFor(c.out, c.errOut).Warn(fmt.Sprintf("rex-daemon was not running on %s; starting it", c.socket))
	}
	c.starter.out, c.starter.errOut = c.out, c.errOut
	return c.starter.run()
}

func daemonLogs(args []string) error {
	fs := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	out := core.AddOutputFlags(fs, core.ContentFormats)
	follow := fs.Bool("f", false, "follow")
	_, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	logPath, err := paths.DaemonLog()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := tailFile(core.Out().Out, logPath, logTailLines, *follow); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
