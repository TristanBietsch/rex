//go:build windows

package loop

import "os/exec"

// configureKill makes cancellation kill the check (Windows has no process
// groups to signal here; children of a hung check may survive).
func configureKill(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return c.Process.Kill()
	}
}
