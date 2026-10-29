package daemonctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrNotRunning is returned when no rex-daemon process is found.
var ErrNotRunning = errors.New("no rex-daemon process found")

// intentReplyWait bounds how long sendIntent waits for a rejection. The
// daemon answers an accepted Shutdown or Reload with nothing (Shutdown also
// closes the connection) and a rejected one with an Error event at once.
const intentReplyWait = 500 * time.Millisecond

// errIntentNotImplemented is the message of daemons that predate an intent.
const errIntentNotImplemented = "intent not implemented"

// intentError is the daemon's Error reply to an intent.
type intentError struct{ msg string }

func (e *intentError) Error() string { return "rex-daemon: " + e.msg }

// unsupported reports a daemon too old to know the intent.
func (e *intentError) unsupported() bool { return e.msg == errIntentNotImplemented }

// sendIntent delivers one intent to the daemon on socket and returns its
// rejection, if any. ErrNotRunning means nothing accepted the connection.
func sendIntent(socket string, send func(*client.Client) error) error {
	c, err := client.Dial(socket)
	if err != nil {
		return ErrNotRunning
	}
	defer c.Close()
	if err := send(c); err != nil {
		return fmt.Errorf("send to rex-daemon: %w", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(intentReplyWait))
	env, err := c.NextEvent()
	if err != nil || env.Type != protocol.EventError {
		return nil
	}
	var e protocol.ErrorEvent
	_ = json.Unmarshal(env.Data, &e)
	return &intentError{msg: e.Message}
}

// waitSocketGone polls until nothing answers on socket, up to StartTimeout,
// so a restart can start a new daemon. Reports whether it went away.
func waitSocketGone(socket string) bool {
	for i := 0; i < StartPollAttempts; i++ {
		if !Reachable(socket) {
			return true
		}
		time.Sleep(StartPollInterval)
	}
	return !Reachable(socket)
}
