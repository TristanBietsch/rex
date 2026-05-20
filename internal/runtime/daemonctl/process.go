package daemonctl

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotRunning is returned when no rex-daemon process is found.
var ErrNotRunning = errors.New("no rex-daemon process found")

// FindPID returns the first PID from pgrep rex-daemon.
func FindPID() (string, error) {
	out, err := exec.Command("pgrep", "rex-daemon").Output()
	if err != nil {
		return "", ErrNotRunning
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return "", ErrNotRunning
	}
	return pid, nil
}

// Reload sends SIGHUP to the running rex-daemon (tools.yaml reload).
func Reload() error {
	pid, err := FindPID()
	if err != nil {
		return err
	}
	if err := exec.Command("kill", "-HUP", pid).Run(); err != nil {
		return fmt.Errorf("kill -HUP %s: %w", pid, err)
	}
	return nil
}

// Stop sends SIGTERM to the running rex-daemon.
func Stop() error {
	pid, err := FindPID()
	if err != nil {
		return err
	}
	if err := exec.Command("kill", "-TERM", pid).Run(); err != nil {
		return fmt.Errorf("kill -TERM %s: %w", pid, err)
	}
	return nil
}

// StopQuietly sends SIGTERM when a daemon is running; ignores lookup/kill errors.
func StopQuietly() {
	pid, err := FindPID()
	if err != nil {
		return
	}
	_ = exec.Command("kill", "-TERM", pid).Run()
}
