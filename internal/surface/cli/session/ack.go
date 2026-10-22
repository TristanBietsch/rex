package session

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ackTimeout bounds how long a CLI command waits for the daemon to confirm.
const ackTimeout = 3 * time.Second

// awaitPatch reads events until the daemon broadcasts a SessionUpdated for
// sessionID carrying patchKey (success), an Error echoing reqID (failure), or
// ackTimeout passes. The connection must already have sent Hello.
func awaitPatch(c *client.Client, sessionID, reqID, patchKey string) error {
	if err := c.SetReadDeadline(time.Now().Add(ackTimeout)); err != nil {
		return core.NewExitError(core.ExitGeneric, err.Error())
	}
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	for {
		env, err := c.NextEvent()
		if err != nil {
			if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") {
				return core.NewExitError(core.ExitGeneric, "daemon did not confirm (is it running a current version? try 'rex daemon restart')")
			}
			return core.NewExitError(core.ExitGeneric, err.Error())
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
			code := core.ExitGeneric
			if ee.Code == protocol.ErrCodeBadIntent {
				code = core.ExitInvalidArgs
			}
			return core.NewExitError(code, msg)
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

// shortDuration renders d with one unit: 45s, 12m, 5h, 6d.
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours())/24) + "d"
	}
}
