package lifecycle

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
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
	socket := core.DefaultSocket()
	if daemonctl.Reachable(socket) {
		fmt.Println("rex-daemon already running")
		return nil
	}
	logf, _ := os.OpenFile(daemonctl.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	res, err := daemonctl.Start(socket, logf)
	if err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	fmt.Printf("rex-daemon started (pid %d)\n", res.PID)
	return nil
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
	fmt.Printf("sent SIGTERM to pid %s\n", pid)
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
	path := daemonctl.LogPath()
	var cmd *exec.Cmd
	if *follow {
		cmd = exec.Command("tail", "-f", path)
	} else {
		cmd = exec.Command("tail", "-n", "200", path)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
