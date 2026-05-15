package cli

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RunDaemon dispatches: rex daemon start | stop | status | restart | logs
func RunDaemon(args []string) error {
	if len(args) == 0 {
		return NewExitError(ExitInvalidArgs, "daemon: subcommand required (start|stop|status|restart|logs)")
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
		return NewExitError(ExitInvalidArgs, fmt.Sprintf("daemon: unknown subcommand %q", args[0]))
	}
}

func daemonStart(args []string) error {
	_ = args
	socket := DefaultSocket()
	if conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		fmt.Println("rex-daemon already running")
		return nil
	}
	cmd := exec.Command(findDaemonBinary())
	logf, lerr := os.OpenFile(daemonLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if lerr == nil {
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		return NewExitError(ExitGeneric, fmt.Sprintf("start: %v", err))
	}
	for i := 0; i < 100; i++ {
		if conn, err := net.Dial("unix", socket); err == nil {
			_ = conn.Close()
			fmt.Printf("rex-daemon started (pid %d)\n", cmd.Process.Pid)
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return NewExitError(ExitDaemonUnreachable, "daemon started but socket didn't appear")
}

func daemonStop(args []string) error {
	_ = args
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return NewExitError(ExitDaemonUnreachable, "no rex-daemon process found")
	}
	if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	fmt.Printf("sent SIGTERM to pid %s\n", pid)
	return nil
}

func daemonStatus(args []string) error {
	_ = args
	socket := DefaultSocket()
	if conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		out, _ := exec.Command("pgrep", "rex-daemon").Output()
		pid := strings.TrimSpace(string(out))
		fmt.Printf("running · socket=%s · pid=%s\n", socket, pid)
		return nil
	}
	fmt.Printf("not running · socket=%s\n", socket)
	return NewExitError(ExitDaemonUnreachable, "")
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
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	path := daemonLogPath()
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

func daemonLogPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "rex", "daemon.log")
}

// findDaemonBinary returns the path to rex-daemon. It checks (in order):
// 1) the same directory as the current rex executable, 2) PATH lookup,
// 3) the literal name "rex-daemon" (so exec gives a useful error).
func findDaemonBinary() string {
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "rex-daemon")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if path, err := exec.LookPath("rex-daemon"); err == nil {
		return path
	}
	return "rex-daemon"
}
