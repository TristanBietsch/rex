package session

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// detachKey is the byte that disconnects us from the session and returns control
// to the caller (the TUI, or the shell that invoked `rex attach`). 0x1d == Ctrl+].
const detachKey byte = 0x1d

// redrawKickTools are TUI agents that own their own screen state and won't
// repaint just because we resized + subscribed — they only redraw on a
// SIGWINCH that genuinely changes geometry, or on Ctrl+L. After we plumb
// stdin/stdout we send Ctrl+L for these so the user sees the current screen
// rather than a stale frame from the replay tail. ollama is intentionally
// excluded — its readline-based prompt treats Ctrl+L as "clear scrollback"
// and loses chat history.
var redrawKickTools = map[string]bool{
	"claude":   true,
	"codex":    true,
	"gemini":   true,
	"grok":     true,
	"deepseek": true,
	"kimi":     true,
}

// RunAttach connects the user's terminal to a session's PTY.
//
// The daemon owns the agent process; we plumb stdin/stdout and the terminal size
// so the agent's TUI renders natively in the caller's terminal. Press Ctrl+] to
// detach. Read-only mode reads stdin only to catch Ctrl+]; nothing is sent.
func RunAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	readOnly := fs.Bool("read-only", false, "attach as observer (no input forwarding)")
	noReplay := fs.Bool("no-replay", false, "skip transcript backlog on attach")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex attach <sel> [-read-only] [-no-replay] [-socket path]"); err != nil {
		return err
	}
	sel := pos[0]

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
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
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
	} else {
		if err := c.SubscribeReplay(sess.ID); err != nil {
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
	}

	// Put terminal in raw mode if stdin is a TTY.
	var oldState *term.State
	if term.IsTerminal(stdinFd) {
		oldState, err = term.MakeRaw(stdinFd)
		if err != nil {
			return core.NewExitError(core.ExitGeneric, fmt.Sprintf("raw mode: %v", err))
		}
		defer func() { _ = term.Restore(stdinFd, oldState) }()
	}

	// Switch to the alt-screen so we don't trample the caller's scrollback,
	// hide the cursor until the agent draws its own, then restore on exit.
	const enterAlt = "\x1b[?1049h\x1b[?25l"
	// exitAlt also turns off mouse tracking / bracketed paste the agent may
	// have enabled (Claude fullscreen does), or the shell is left in mouse mode.
	const exitAlt = "\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?25h\x1b[?1049l"
	restoreVT := enableVT(os.Stdout)
	defer restoreVT()
	_, _ = os.Stdout.WriteString(enterAlt)
	defer func() { _, _ = os.Stdout.WriteString(exitAlt) }()

	// Redraw kick: full-TUI agents (codex/claude/gemini) won't repaint on
	// their own — replay shows a frozen frame from the transcript tail and
	// SIGWINCH on a same-size resize is a no-op. A delayed Ctrl+L makes
	// them redraw current screen state. Delay is tiny so the replay flush
	// has a chance to land first; otherwise we kick before the agent even
	// sees the resize. The kick is PTY input, so read-only never sends it.
	if redrawKickTools[sess.ToolID] && !*readOnly {
		go func(sessID, toolID string) {
			time.Sleep(150 * time.Millisecond)
			if err := c.SendRedraw(sessID); err != nil {
				slog.Warn("attach: redraw kick failed", "session", sessID, "tool", toolID, "err", err)
				return
			}
			slog.Info("attach: redraw kick sent", "session", sessID, "tool", toolID)
		}(sess.ID, sess.ToolID)
	}

	// Terminal resize → resize the daemon's PTY.
	stopResize := onResize(func() {
		if w, h, gerr := term.GetSize(stdoutFd); gerr == nil && w > 0 && h > 0 {
			_ = c.Resize(sess.ID, uint16(w), uint16(h))
		}
	})
	defer stopResize()

	// stdin → daemon, watching for Ctrl+] to detach. Read-only still reads:
	// in raw mode the terminal sends Ctrl+] only to us.
	go pumpInput(os.Stdin, *readOnly, func(b []byte) { _ = c.SendInput(sess.ID, b) }, func() {
		_ = c.Close() // unblock the event loop
	})

	// daemon → stdout.
	for {
		env, err := c.NextEvent()
		if err != nil {
			return nil
		}
		switch env.Type {
		case protocol.EventSessionOutput:
			var so protocol.SessionOutput
			if jerr := json.Unmarshal(env.Data, &so); jerr == nil && so.SessionID == sess.ID {
				_, _ = os.Stdout.Write(so.Bytes)
			}
		case protocol.EventSessionRemoved:
			var rm protocol.SessionRemoved
			if jerr := json.Unmarshal(env.Data, &rm); jerr == nil && rm.SessionID == sess.ID {
				slog.Info("attach: session removed", "session", sess.ID, "reason", rm.Reason)
				msg := "session deleted"
				if rm.Reason == protocol.RemovedRetired {
					msg = "session cleared (marked done)"
				}
				return core.NewExitError(core.ExitGeneric, msg)
			}
		}
	}
}

// pumpInput reads r until detachKey or a read error, then calls detach.
// Bytes before the key go to send unless readOnly, which discards them. A
// read-only observer whose stdin ends (e.g. </dev/null) stays attached.
func pumpInput(r io.Reader, readOnly bool, send func([]byte), detach func()) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			idx := indexByte(chunk, detachKey)
			if idx >= 0 {
				chunk = chunk[:idx]
			}
			if len(chunk) > 0 && !readOnly {
				send(chunk)
			}
			if idx >= 0 {
				detach()
				return
			}
		}
		if err != nil {
			if !readOnly {
				detach()
			}
			return
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
