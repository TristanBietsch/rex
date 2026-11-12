//go:build windows

package registry

import (
	"os/exec"
	"strconv"
)

// configureKill makes a timeout or cancel kill the command and its children
// (taskkill /T walks the process tree).
func configureKill(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid)) //nolint:gosec // pid of our own child
		if err := kill.Run(); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
}
