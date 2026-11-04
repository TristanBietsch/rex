package lifecycle

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
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
	case "stop":
		return daemonStop(args[1:])
	case "status":
		return daemonStatus(args[1:])
	case "restart":
		return daemonRestart(args[1:])
	case "logs":
		return daemonLogs(args[1:])
	default:
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("daemon: unknown subcommand %q", args[0]))
	}
}

func daemonStart(args []string) error {
	_ = args
	return daemonStarter{
		socket:    core.DefaultSocket(),
		logPath:   daemonctl.LogPath(),
		reachable: daemonctl.Reachable,
		start:     daemonctl.Start,
		out:       os.Stdout,
	}.run()
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

// openDaemonLog opens the rex-daemon stderr log for appending. The
// directory is created 0700 and the file is 0600, tightening a log an older
// version left world-readable: panics and stderr output can carry session
// paths and agent output (backlog S-01).
func openDaemonLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create daemon log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon log: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("restrict daemon log %s: %w", path, err)
	}
	return f, nil
}

func daemonStop(args []string) error {
	_ = args
	pid, err := daemonctl.FindPID()
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	if err := daemonctl.Stop(); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	fmt.Printf("stopped rex-daemon (pid %s)\n", pid)
	return nil
}

func daemonStatus(args []string) error {
	_ = args
	socket := core.DefaultSocket()
	if conn, err := net.DialTimeout("unix", socket, daemonctl.SocketDialTimeout); err == nil {
		_ = conn.Close()
		pid, _ := daemonctl.FindPID()
		fmt.Printf("running · socket=%s · pid=%s\n", socket, pid)
		return nil
	}
	fmt.Printf("not running · socket=%s\n", socket)
	return core.NewExitError(core.ExitDaemonUnreachable, "")
}

func daemonRestart(args []string) error {
	if err := daemonStop(args); err != nil {
		fmt.Fprintln(os.Stderr, "stop:", err)
	}
	time.Sleep(300 * time.Millisecond)
	return daemonStart(args)
}

func daemonLogs(args []string) error {
	fs := flag.NewFlagSet("daemon logs", flag.ContinueOnError)
	follow := fs.Bool("f", false, "follow")
	if err := fs.Parse(args); err != nil {
		return core.NewExitError(core.ExitInvalidArgs, err.Error())
	}
	if err := tailFile(os.Stdout, daemonctl.LogPath(), logTailLines, *follow); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	return nil
}
