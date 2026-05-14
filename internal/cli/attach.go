package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunAttach connects the user's terminal to a session's PTY.
//
// Detach sequence: ctrl+a then d (same as tmux/screen). Read-only mode skips
// stdin forwarding.
func RunAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	readOnly := fs.Bool("read-only", false, "attach as observer (no input forwarding)")
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

	if err := c.Subscribe(sess.ID); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}

	// Put terminal in raw mode if stdin is a TTY.
	fd := int(os.Stdin.Fd())
	var oldState *term.State
	if term.IsTerminal(fd) {
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return NewExitError(ExitGeneric, fmt.Sprintf("raw mode: %v", err))
		}
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	fmt.Fprintf(os.Stderr, "\r\n[attached to %s — ctrl+a d to detach]\r\n", sess.Slug)

	// stdin → SendInput, watching for ctrl+a d.
	stdinDone := make(chan struct{})
	if !*readOnly {
		go func() {
			defer close(stdinDone)
			buf := make([]byte, 1024)
			var prev byte
			for {
				n, rerr := os.Stdin.Read(buf)
				if n > 0 {
					chunk := buf[:n]
					if containsDetachSeq(chunk, prev) {
						return
					}
					prev = chunk[n-1]
					_ = c.SendInput(sess.ID, chunk)
				}
				if rerr != nil {
					return
				}
			}
		}()
	} else {
		close(stdinDone)
	}

	// daemon events → stdout.
	for {
		select {
		case <-stdinDone:
			fmt.Fprintf(os.Stderr, "\r\n[detached]\r\n")
			return nil
		default:
		}
		env, err := c.NextEvent()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\r\n[connection closed]\r\n")
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

// containsDetachSeq returns true if `chunk` (with a hint of the previous chunk's
// last byte) contains the ctrl+a d sequence.
func containsDetachSeq(chunk []byte, prevByte byte) bool {
	if len(chunk) == 0 {
		return false
	}
	if prevByte == 0x01 && chunk[0] == 'd' {
		return true
	}
	for i := 0; i+1 < len(chunk); i++ {
		if chunk[i] == 0x01 && chunk[i+1] == 'd' {
			return true
		}
	}
	return false
}
