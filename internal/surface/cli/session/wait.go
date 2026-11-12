package session

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitTargets are the values -until accepts.
var waitTargets = []string{"working", "needs_input", "done", "failed", "any"}

// RunWait blocks until a session reaches a target state.
func RunWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	until := fs.String("until", "done", "target state: working | needs_input | done | failed | any")
	timeout := fs.Duration("timeout", 0, "max wait duration (0 = no timeout)")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "wait: exactly one selector required")
	}
	sel := rest[0]
	if !slices.Contains(waitTargets, *until) {
		return core.NewExitError(core.ExitInvalidArgs,
			fmt.Sprintf("wait: -until %q: want one of %s", *until, strings.Join(waitTargets, ", ")))
	}
	if *timeout < 0 {
		return core.NewExitError(core.ExitInvalidArgs, fmt.Sprintf("wait: -timeout %s: must not be negative", *timeout))
	}

	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
	if err != nil {
		return err
	}

	if matchesState(sess.State, *until) {
		return emitWait(sess, sess.State)
	}

	deadline := time.Time{}
	if *timeout > 0 {
		deadline = time.Now().Add(*timeout)
	}

	for {
		if !deadline.IsZero() {
			if err := c.SetReadDeadline(deadline); err != nil {
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
		}
		env, err := c.NextEvent()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return core.NewExitError(core.ExitWaitTimedOut, "wait timed out")
			}
			return core.NewExitError(core.ExitGeneric, err.Error())
		}
		if env.Type == protocol.EventSessionRemoved {
			var rm protocol.SessionRemoved
			if json.Unmarshal(env.Data, &rm) == nil && rm.SessionID == sess.ID {
				return core.NewExitError(core.ExitSelectorNotFound, "session removed")
			}
			continue
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
			return emitWait(sess, protocol.State(stateStr))
		}
	}
}

func matchesState(actual protocol.State, target string) bool {
	if target == "any" {
		switch actual {
		case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
			return true
		case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
			return false
		}
		return false
	}
	return string(actual) == target
}

// waitData is the `-o json` payload of rex wait.
type waitData struct {
	actionData
	State protocol.State `json:"state"`
}

// emitWait reports the state the wait ended on. txt stays silent: the exit
// code is the answer.
func emitWait(s protocol.SessionSummary, state protocol.State) error {
	text := s.Slug + " is " + string(state)
	if state == protocol.StateNeedsInput {
		text = s.Slug + " needs you"
	}
	return emitAction("wait", waitData{refOf(s), state}, text, "",
		sessionFields(s, core.Field{Key: "state", Val: string(state)}))
}
