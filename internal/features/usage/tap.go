package usage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TapCap bounds how long the tap spends sending to the daemon, dial and
// write together. The send runs beside the user's own command, so the tap
// adds at most this on top of it, and only when the daemon hangs.
const TapCap = 200 * time.Millisecond

// tapGrace is the slack after TapCap before the tap stops waiting for a
// sender that ignored its deadline.
const tapGrace = 30 * time.Millisecond

// sessionIDPattern is what a rex session id looks like; the tap sends
// nothing for any other --session value.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// TapOptions configures RunTap. Zero fields take their production default.
type TapOptions struct {
	// Session is the rex session id (--session).
	Session string
	// Socket is the daemon socket; "" resolves the default.
	Socket string
	// Stdin, Stdout and Stderr are the process's streams.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Home locates the user's Claude settings (~/.claude); "" uses the
	// user's home directory.
	Home string
	// Send delivers the sample to the daemon (nil dials Socket). It must
	// honor ctx; RunTap also stops waiting after TapCap.
	Send func(ctx context.Context, s protocol.UsageSample) error
	// Cap overrides TapCap (tests).
	Cap time.Duration
}

// RunTap is `rex-daemon statusline-tap`: Claude Code's status-line command.
//
// It reads the status-line JSON from stdin, sends the usage fields to the
// daemon (asynchronously, never past the cap), and runs the user's own
// status-line command with the identical stdin bytes, passing its stdout and
// stderr through and returning its exit code. With no user command it prints
// nothing and returns 0. A daemon that is down or hung never stops the user's
// command from running. Nothing is ever written to a Claude settings file.
func RunTap(ctx context.Context, o TapOptions) int {
	capD := o.Cap
	if capD <= 0 {
		capD = TapCap
	}
	in, _ := io.ReadAll(o.Stdin) // a short read still forwards what arrived

	sendDone := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(sendDone)
		o.sendSample(ctx, in, capD)
	}()

	code := 0
	if cmd := o.userCommand(); cmd != "" {
		code = runUser(ctx, cmd, in, o.Stdout, o.Stderr)
	}

	// The user's command ran beside the send; wait only for what is left.
	left := capD + tapGrace - time.Since(started)
	if left < 0 {
		left = 0
	}
	t := time.NewTimer(left)
	defer t.Stop()
	select {
	case <-sendDone:
	case <-t.C:
	}
	return code
}

// sendSample parses in and delivers its usage fields, best effort.
func (o TapOptions) sendSample(ctx context.Context, in []byte, capD time.Duration) {
	if !sessionIDPattern.MatchString(o.Session) {
		return
	}
	s, ok := ParseStatusLine(in)
	if !ok || s.Empty() {
		return
	}
	s.SessionID = o.Session
	ctx, cancel := context.WithTimeout(ctx, capD)
	defer cancel()
	send := o.Send
	if send == nil {
		send = func(ctx context.Context, s protocol.UsageSample) error { return dialAndSend(ctx, o.Socket, s) }
	}
	_ = send(ctx, s)
}

// dialAndSend writes one UsageSample intent (no ID, no reply awaited).
func dialAndSend(ctx context.Context, socket string, s protocol.UsageSample) error {
	if socket == "" {
		var err error
		if socket, err = paths.Socket(); err != nil {
			return err
		}
	}
	c, err := client.DialContext(ctx, socket)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Send(ctx, protocol.IntentUsageSample, s)
}

// userCommand finds the user's own status-line command: from the user-level
// Claude settings only, never from the folder or from anything in the
// status-line JSON.
func (o TapOptions) userCommand() string {
	home := o.Home
	if home == "" {
		home, _ = paths.Home()
	}
	return UserStatusLine(home)
}

// runUser runs the user's command through a shell, as Claude would, with in
// on stdin, and returns its exit code. A command that cannot start exits 127.
func runUser(ctx context.Context, command string, in []byte, stdout, stderr io.Writer) int {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "bash", "-c", command)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command)
	}
	c.Stdin = bytes.NewReader(in)
	c.Stdout, c.Stderr = stdout, stderr
	err := c.Run()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return max(ee.ExitCode(), 1) // a signal reads as -1
	}
	return 127
}
