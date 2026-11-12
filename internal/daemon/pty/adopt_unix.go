//go:build !windows

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// adoptConsole wraps a child handed over by the previous daemon image. The
// child is still this process's child (same pid across execve), so Wait
// reaps it and returns its exit status as for a spawned one.
func adoptConsole(a *Adopted, argv []string) (*exec.Cmd, console, error) {
	if a.File == nil || a.PID <= 0 {
		return nil, nil, errors.New("adopt: no PTY or pid")
	}
	proc, err := os.FindProcess(a.PID)
	if err != nil {
		return nil, nil, fmt.Errorf("adopt pid %d: %w", a.PID, err)
	}
	cmd := &exec.Cmd{Process: proc}
	if len(argv) > 0 {
		cmd.Path, cmd.Args = argv[0], argv
	}
	return cmd, unixConsole{File: a.File, cmd: cmd}, nil
}
