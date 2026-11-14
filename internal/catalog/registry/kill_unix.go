//go:build !windows

package registry

import (
	"os/exec"
	"syscall"
)

// configureKill puts the command in its own process group and makes a
// timeout or cancel kill the whole group, so a shell's children go with it.
func configureKill(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
