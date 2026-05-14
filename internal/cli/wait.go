package cli

import (
	"encoding/json"
	"flag"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// RunWait blocks until a session reaches a target state.
func RunWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	socket := fs.String("socket", DefaultSocket(), "UDS path")
	until := fs.String("until", "done", "target state: working | needs_input | done | failed | any")
	timeout := fs.Duration("timeout", 0, "max wait duration (0 = no timeout)")
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "wait: exactly one selector required")
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

	if matchesState(sess.State, *until) {
		return nil
	}

	deadline := time.Time{}
	if *timeout > 0 {
		deadline = time.Now().Add(*timeout)
	}

	for {
		if !deadline.IsZero() {
			if err := c.SetReadDeadline(deadline); err != nil {
				return NewExitError(ExitGeneric, err.Error())
			}
		}
		env, err := c.NextEvent()
		if err != nil {
			if strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "i/o timeout") {
				return NewExitError(ExitWaitTimedOut, "wait timed out")
			}
			return NewExitError(ExitGeneric, err.Error())
		}
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var upd protocol.SessionUpdated
		if err := json.Unmarshal(env.Data, &upd); err != nil {
			continue
		}
		if upd.SessionID != sess.ID {
			continue
		}
		stateStr, _ := upd.Patch["state"].(string)
		if stateStr == "" {
			continue
		}
		if matchesState(protocol.State(stateStr), *until) {
			return nil
		}
	}
}

func matchesState(actual protocol.State, target string) bool {
	if target == "any" {
		switch actual {
		case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
			return true
		}
		return false
	}
	return string(actual) == target
}
