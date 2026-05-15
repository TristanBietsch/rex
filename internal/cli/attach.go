package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// detachKey is the byte that disconnects us from the session and returns control
// to the caller (the TUI, or the shell that invoked `rex attach`). 0x1d == Ctrl+].
const detachKey byte = 0x1d

// RunAttach connects the user's terminal to a session's PTY.
//
// The daemon owns the agent process; we plumb stdin/stdout and the terminal size
// so the agent's TUI renders natively in the caller's terminal. Press Ctrl+] to
// detach. Read-only mode skips stdin forwarding.
func RunAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	readOnly := fs.Bool("read-only", false, "attach as observer (no input forwarding)")
	noReplay := fs.Bool("no-replay", false, "skip transcript backlog on attach")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "attach: exactly one selector required")
	}
	sel := fs.Arg(0)

	c, err := client.Dial(*socket)
	if err != nil {
		return NewExitError(ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	stdoutFd := int(os.Stdout.Fd())
	stdinFd := int(os.Stdin.Fd())

	// Size the daemon's PTY to our terminal before subscribing so the agent's
	// first redraw lands at the right geometry.
	if term.IsTerminal(stdoutFd) {
		if w, h, gerr := term.GetSize(stdoutFd); gerr == nil && w > 0 && h > 0 {
			_ = c.Resize(sess.ID, uint16(w), uint16(h))
		}
	}

	if *noReplay {
		if err := c.Subscribe(sess.ID); err != nil {
			return NewExitError(ExitGeneric, err.Error())
		}
	} else {
		if err := c.SubscribeReplay(sess.ID); err != nil {
			return NewExitError(ExitGeneric, err.Error())
		}
	}

	// Put terminal in raw mode if stdin is a TTY.
	var oldState *term.State
	if term.IsTerminal(stdinFd) {
		oldState, err = term.MakeRaw(stdinFd)
		if err != nil {
			return NewExitError(ExitGeneric, fmt.Sprintf("raw mode: %v", err))
		}
		defer func() { _ = term.Restore(stdinFd, oldState) }()
	}

	// Switch to the alt-screen so we don't trample the caller's scrollback,
	// hide the cursor until the agent draws its own, then restore on exit.
	const enterAlt = "\x1b[?1049h\x1b[?25l"
	const exitAlt = "\x1b[?25h\x1b[?1049l"
	_, _ = os.Stdout.WriteString(enterAlt)
	defer func() { _, _ = os.Stdout.WriteString(exitAlt) }()

	// SIGWINCH → resize the daemon's PTY.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if w, h, gerr := term.GetSize(stdoutFd); gerr == nil && w > 0 && h > 0 {
				_ = c.Resize(sess.ID, uint16(w), uint16(h))
			}
		}
	}()

	// stdin → daemon, watching for Ctrl+] to detach.
	if !*readOnly {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, rerr := os.Stdin.Read(buf)
				if n > 0 {
					chunk := buf[:n]
					if idx := indexByte(chunk, detachKey); idx >= 0 {
						if idx > 0 {
							_ = c.SendInput(sess.ID, chunk[:idx])
						}
						_ = c.Close() // unblock the event loop
						return
					}
					_ = c.SendInput(sess.ID, chunk)
				}
				if rerr != nil {
					_ = c.Close()
					return
				}
			}
		}()
	}

	// daemon → stdout.
	for {
		env, err := c.NextEvent()
		if err != nil {
			return nil
		}
		if env.Type == protocol.EventSessionOutput {
			var so protocol.SessionOutput
			if jerr := json.Unmarshal(env.Data, &so); jerr == nil && so.SessionID == sess.ID {
				_, _ = os.Stdout.Write(so.Bytes)
			}
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
