package core

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ackTimeout bounds how long a CLI command waits for the daemon to confirm.
const ackTimeout = 3 * time.Second

// AwaitPatch reads events until the daemon broadcasts a SessionUpdated for
// sessionID carrying patchKey (success), an Error echoing reqID (failure), or
// ackTimeout passes. The connection must already have sent Hello.
func AwaitPatch(c *client.Client, sessionID, reqID, patchKey string) error {
	if err := c.SetReadDeadline(time.Now().Add(ackTimeout)); err != nil {
		return NewExitError(ExitGeneric, err.Error())
	}
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	for {
		env, err := c.NextEvent()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return NewExitError(ExitGeneric, "daemon did not confirm (is it running a current version? try 'rex daemon restart')")
			}
			return NewExitError(ExitGeneric, err.Error())
		}
		switch env.Type {
		case protocol.EventError:
			var ee protocol.ErrorEvent
			if json.Unmarshal(env.Data, &ee) != nil || ee.ID != reqID {
				continue
			}
			slog.Debug("cli: intent rejected", "req_id", reqID, "code", ee.Code, "message", ee.Message)
			msg := ee.Message
			if ee.Code == protocol.ErrCodeBadIntent && msg == "intent not implemented" {
				msg = "daemon is older than rex — run 'rex daemon restart'"
			}
			code := ExitGeneric
			if ee.Code == protocol.ErrCodeBadIntent {
				code = ExitInvalidArgs
			}
			return NewExitError(code, msg)
		case protocol.EventSessionUpdated:
			var u protocol.SessionUpdated
			if json.Unmarshal(env.Data, &u) != nil || u.SessionID != sessionID {
				continue
			}
			if _, ok := u.Patch[patchKey]; ok {
				return nil
			}
		}
	}
}
