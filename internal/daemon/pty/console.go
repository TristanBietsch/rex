package pty

import "io"

// console is the supervisor's end of a child's pseudo-terminal: a Unix PTY
// master (console_unix.go) or a Windows ConPTY (console_windows.go). It owns
// the child started on it, so reaping and killing go through it too.
type console interface {
	io.ReadWriteCloser
	Resize(cols, rows uint16) error
	// Wait reaps the child. Call it once, after the reader drains or the
	// child is killed. Non-zero exits return *exec.ExitError.
	Wait() error
	// KillTree kills the child and every process it spawned (agents start MCP
	// servers and tool shells that inherit the terminal).
	KillTree() error
}
