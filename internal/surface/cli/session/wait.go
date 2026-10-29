package session

import (
	"encoding/json"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitTargets are the accepted -until values. "any" matches any terminal
// state (done, failed, crashed).
var waitTargets = []string{"working", "needs_input", "done", "failed", "crashed", "any"}

// RunWait blocks until a session reaches a target state.
func RunWait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	socket := fs.String("socket", core.DefaultSocket(), "UDS path")
	until := fs.String("until", "done", "target state: "+strings.Join(waitTargets, " | ")+" (any = done, failed, or crashed)")
	timeout := fs.Duration("timeout", 0, "max wait duration (0 = no timeout)")
	pos, err := core.ParseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := core.CheckArgs(pos, 1, 1, "rex wait <sel> [-until state] [-timeout duration] [-socket path]"); err != nil {
		return err
	}
	sel := pos[0]
	// A typo would never match, blocking forever with the default timeout.
	if !slices.Contains(waitTargets, *until) {
		return core.NewExitError(core.ExitInvalidArgs,
			fmt.Sprintf("wait: unknown -until %q (want %s)", *until, strings.Join(waitTargets, ", ")))
	}

	c, err := client.Dial(*socket)
	if err != nil {
		return core.NewExitError(core.ExitDaemonUnreachable, err.Error())
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, sel)
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
				return core.NewExitError(core.ExitGeneric, err.Error())
			}
		}
		env, err := c.NextEvent()
		if err != nil {
			if strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "i/o timeout") {
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
