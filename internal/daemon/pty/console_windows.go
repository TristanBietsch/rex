//go:build windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/charmbracelet/x/xpty"
	"golang.org/x/sys/windows"
)

// conptyFlushDelay lets conhost forward the child's last frame before the
// pseudo console is closed.
const conptyFlushDelay = 100 * time.Millisecond

// winConsole is a ConPTY (Windows 10 1809+). The child is attached to the
// pseudo console and its output arrives as VT sequences, like a Unix PTY.
//
// ConPTY keeps its output pipe open after the child exits, so the reader
// would never see EOF. A waiter goroutine reaps the child and then closes the
// pseudo console, which ends the reader the way a Unix PTY hangup does.
type winConsole struct {
	*xpty.ConPty
	cmd     *exec.Cmd
	exited  chan struct{}
	waitErr error
}

// startConsole starts cmd attached to a new ConPTY. os/exec cannot attach a
// pseudo console, so conpty spawns the process and sets cmd.Process.
func startConsole(cmd *exec.Cmd, cols, rows uint16) (console, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	cp, err := xpty.NewConPty(int(cols), int(rows))
	if err != nil {
		return nil, fmt.Errorf("conpty: %w", err)
	}
	if err := cp.Start(cmd); err != nil {
		_ = cp.Close()
		return nil, err
	}
	c := &winConsole{ConPty: cp, cmd: cmd, exited: make(chan struct{})}
	go func() {
		c.waitErr = xpty.WaitProcess(context.Background(), cmd)
		close(c.exited)
		time.Sleep(conptyFlushDelay)
		_ = c.ConPty.Close()
	}()
	return c, nil
}

// Read maps the pipe errors a closed pseudo console produces to io.EOF.
func (c *winConsole) Read(b []byte) (int, error) {
	n, err := c.ConPty.Read(b)
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_INVALID_HANDLE) || errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
		err = io.EOF
	}
	return n, err
}

func (c *winConsole) Resize(cols, rows uint16) error {
	return c.ConPty.Resize(int(cols), int(rows))
}

func (c *winConsole) Wait() error {
	<-c.exited
	return c.waitErr
}

// KillTree has taskkill walk the process tree; Windows has no process group
// to signal here. Once the child is reaped it is a no-op: its pid may already
// belong to an unrelated process, and taskkill can't walk the tree of a
// parent that is gone.
func (c *winConsole) KillTree() error {
	select {
	case <-c.exited:
		return nil
	default:
	}
	pid := strconv.Itoa(c.cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		return fmt.Errorf("taskkill /T /F /PID %s: %w", pid, err)
	}
	return nil
}
