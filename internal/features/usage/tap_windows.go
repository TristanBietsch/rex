//go:build windows

package usage

import "os/exec"

// configureKill makes cancellation kill the user's command (Windows has no
// process groups to signal here).
func configureKill(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return c.Process.Kill()
	}
}
