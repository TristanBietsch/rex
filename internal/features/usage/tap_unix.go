//go:build !windows

package usage

import (
	"os/exec"
	"syscall"
)

// configureKill puts the user's command in its own process group and makes
// cancellation kill the whole group, so a shell's children go with it.
func configureKill(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
