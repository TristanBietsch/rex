//go:build !windows

package loop

import (
	"os/exec"
	"syscall"
)

// configureKill puts the check in its own process group and makes
// cancellation kill the whole group, so children it spawned go with it.
func configureKill(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
