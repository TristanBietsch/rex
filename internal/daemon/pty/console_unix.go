//go:build !windows

package pty

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// unixConsole is a PTY master; the child holds the slave as its controlling tty.
type unixConsole struct {
	*os.File
	cmd *exec.Cmd
}

// startConsole starts cmd on a new PTY (creack/pty runs it with Setsid).
func startConsole(cmd *exec.Cmd, cols, rows uint16) (console, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	return unixConsole{File: f, cmd: cmd}, nil
}

func (c unixConsole) Resize(cols, rows uint16) error {
	return pty.Setsize(c.File, &pty.Winsize{Cols: cols, Rows: rows})
}

func (c unixConsole) Wait() error { return c.cmd.Wait() }

// isHangup reports whether a PTY master read error means the slave side was
// closed. Linux returns EIO instead of EOF once the child (and everything
// that inherited the slave) has exited; that is a normal end of session, not
// a reader failure.
func isHangup(err error) bool { return errors.Is(err, syscall.EIO) }

// KillTree SIGKILLs the child's process group (Setsid, so pgid == pid).
func (c unixConsole) KillTree() error {
	if err := syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
