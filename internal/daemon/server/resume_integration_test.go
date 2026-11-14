package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A session that ran to done is resumed through the client SDK: the same
// row comes back queued with ended_at cleared, the child is started with
// --resume <its conversation id>, and the session reaches done again with the
// transcript holding both runs.
func TestClientResume_DoneSessionRunsAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a daemon and a PTY child")
	}
	sum, _, d := runToDone(t, resumeToolsYAML, "argv", false)
	require.Contains(t, readTranscript(t, d, sum.ID), "ARGV: --session-id "+sum.ResumeID)

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	require.NoError(t, c.Resume(sum.ID, "r1"))
	var queued, doneAgain bool
	for !doneAgain {
		env, err := c.NextEventContext(ctx)
		require.NoError(t, err)
		switch env.Type {
		case protocol.EventError:
			t.Fatalf("resume failed: %s", env.Data)
		case protocol.EventSessionUpdated:
			var up protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &up))
			switch up.Patch["state"] {
			case string(protocol.StateQueued):
				queued = true
				v, ok := up.Patch["ended_at"]
				require.True(t, ok)
				require.Nil(t, v)
			case string(protocol.StateDone):
				doneAgain = queued
			}
		}
	}

	tr := readTranscript(t, d, sum.ID)
	require.Equal(t, 1, strings.Count(tr, "ARGV: --session-id "+sum.ResumeID), tr)
	require.Equal(t, 1, strings.Count(tr, "ARGV: --resume "+sum.ResumeID), tr)
	meta := readMeta(t, d, sum.ID)
	require.Equal(t, sum.ResumeID, meta.ResumeID)
	require.Equal(t, protocol.StateDone, meta.State)
	require.False(t, meta.EndedAt.IsZero())
}
