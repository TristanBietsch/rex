package tui

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// resumableTools is a fake agent that takes a preassigned conversation id
// (--session-id, as Claude does), prints its argv and exits shortly after.
const resumableTools = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
      new_args: ["--session-id", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
`

// pumpUntil feeds daemon events into the model until cond holds.
func pumpUntil(t *testing.T, c *client.Client, m Model, what string, cond func(Model) bool) Model {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for !cond(m) {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err, "waiting for %s", what)
		require.NotEqual(t, protocol.EventError, env.Type, "daemon error: %s", env.Data)
		m = m.applyEvent(env)
	}
	return m
}

// TestUC_E2E_ResumesExitedSession drives uc against a real daemon: a session
// that ran to done is respawned in place, its row leaves Done for the
// working group, and it finishes again.
func TestUC_E2E_ResumesExitedSession(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	d := testutil.StartDaemonWithTools(t, resumableTools)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.NewSession(protocol.NewSession{ToolID: "echo", ModelID: "argv", Slug: "again", CWD: d.StateDir}))

	m := boardModel()
	m.Client = c
	row := func(m Model) protocol.SessionSummary {
		if len(m.Sessions) == 0 {
			return protocol.SessionSummary{}
		}
		return m.Sessions[0]
	}
	m = pumpUntil(t, c, m, "first run to finish", func(m Model) bool { return row(m).State == protocol.StateDone })
	id := row(m).ID
	require.True(t, protocol.ValidResumeID(row(m).ResumeID), "the daemon preassigned a resume id")
	require.True(t, inCompleted(row(m)), "an exited session sits in Done")
	m.SelectedID = id

	m = typeKeys(m, "u")
	m, cmd := updateKey(m, key("c"))
	require.Empty(t, m.Err)
	require.Contains(t, m.Notice, "resuming again (resume id "+row(m).ResumeID[:8]+"…)")
	require.NotNil(t, cmd)
	require.Nil(t, cmd(), "the intent was accepted")

	m = pumpUntil(t, c, m, "the row to leave Done", func(m Model) bool { return !inCompleted(row(m)) })
	require.Nil(t, row(m).ExitCode, "exit_code: null patch clears the old exit")
	require.True(t, row(m).EndedAt.IsZero(), "ended_at: null patch")
	m = pumpUntil(t, c, m, "the second run to finish", func(m Model) bool { return row(m).State == protocol.StateDone })
	require.Equal(t, id, row(m).ID, "same row")
}
