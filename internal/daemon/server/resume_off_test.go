package server_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func resumeOff(cfg *server.Config) { cfg.ResumeEnabled = func() bool { return false } }

// offToolsYAML has a preassign tool (argv) and a capture_regex tool (bye).
const offToolsYAML = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
      new_args: ["--session-id", "{id}"]
      capture_regex: "resume with id=([0-9a-fA-F-]{36})"
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.2']
      - id: bye
        name: "Prints its id on exit"
        args: ['echo working; sleep 0.2; echo "resume with id=7D4F3C8A-1234-4ABC-89AB-CDEFABCDEF01"']
`

// With the resume feature off the daemon is inert about resume ids: no
// --session-id preassign, no resume_id on the session, no capture, and a
// Resume intent is refused with bad_intent "resume is off".
func TestResumeOff_IsInert(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithConfig(t, offToolsYAML, resumeOff)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	start := func(model, slug string) protocol.SessionSummary {
		_, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: model, Slug: slug, CWD: d.StateDir})
		require.NoError(t, err)
		var sum protocol.SessionSummary
		for {
			env, err := c.NextEventContext(ctx)
			require.NoError(t, err)
			switch env.Type {
			case protocol.EventSessionAdded:
				require.NoError(t, json.Unmarshal(env.Data, &sum))
			case protocol.EventSessionUpdated:
				var up protocol.SessionUpdated
				require.NoError(t, json.Unmarshal(env.Data, &up))
				require.NotContains(t, up.Patch, "resume_id")
				if up.Patch["state"] == string(protocol.StateDone) {
					return sum
				}
			}
		}
	}

	sum := start("argv", "off1")
	require.Empty(t, sum.ResumeID)
	require.NotContains(t, readTranscript(t, d, sum.ID), "--session-id")
	require.Empty(t, readMeta(t, d, sum.ID).ResumeID)

	captured := start("bye", "off2")
	require.Empty(t, readMeta(t, d, captured.ID).ResumeID, "capture_regex ignored")

	require.NoError(t, c.Resume(sum.ID, "r1"))
	for {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err)
		if env.Type != protocol.EventError {
			continue
		}
		var e protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &e))
		require.Equal(t, protocol.ErrCodeBadIntent, e.Code)
		require.Equal(t, "resume is off", e.Message)
		break
	}
}

// A hook-detected agent's sid lines are ignored while resume is off.
func TestResumeOff_HookSidIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithConfig(t, hookToolsYAML, resumeOff)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "sid", Slug: "hook", CWD: d.StateDir})
	require.NoError(t, err)
	for {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err)
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var up protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &up))
		require.NotContains(t, up.Patch, "resume_id")
		if up.Patch["state"] == string(protocol.StateDone) {
			return
		}
	}
}
