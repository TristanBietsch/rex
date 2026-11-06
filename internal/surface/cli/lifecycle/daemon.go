package lifecycle

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// RunDaemon dispatches: rex daemon start | stop | status | restart | logs
func RunDaemon(args []string) error {
	if len(args) == 0 {
		return core.NewExitError(core.ExitInvalidArgs, "daemon: subcommand required (start|stop|status|restart|logs)")
	}
	switch args[0] {
	case "start":
		return daemonStart(args[1:])
	case "stop", "status", "restart":
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

func daemonStart(args []string) error {
	_ = args
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
		out:       os.Stdout,
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
}

// run starts the daemon unless one already answers on the socket. The
// daemon's stderr goes to logPath; failing to open it is an error rather
// than a silently discarded crash log.
func (s daemonStarter) run() error {
	if s.reachable(s.socket) {
		fmt.Fprintln(s.out, "rex-daemon already running")
		return nil
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
	fmt.Fprintf(s.out, "rex-daemon started (pid %d)\n", res.PID)
	return nil
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
		out:       os.Stdout,
		errOut:    os.Stderr,
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
	fmt.Fprintf(c.out, "stopped rex-daemon (pid %s) · socket=%s\n", pid, c.socket)
	return nil
}

// status reports whether the daemon answers on the socket. That is the
// truth: a rex-daemon process that does not answer here (another socket,
// another HOME) does not count, and a failed PID lookup only shows as
// pid=unknown.
func (c daemonCtl) status() error {
	if !c.reachable(c.socket) {
		fmt.Fprintf(c.out, "not running · socket=%s\n", c.socket)
		return core.NewExitError(core.ExitDaemonUnreachable, "")
	}
	fmt.Fprintf(c.out, "running · socket=%s · pid=%s\n", c.socket, c.pid())
	return nil
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
		fmt.Fprintf(c.errOut, "rex-daemon was not running on %s; starting it\n", c.socket)
	}
	return c.starter.run()
}

func daemonLogs(args []string) error {
	fs := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "follow")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	logPath, err := paths.DaemonLog()
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	if err := tailFile(os.Stdout, logPath, logTailLines, *follow); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
