package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// rex prune against a real in-process daemon: a finished echo session is
// deleted (and acknowledged), one still waiting for input is left alone.
func TestRunPrune_RealDaemon(t *testing.T) {
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("prune-test")
	require.NoError(t, err)
	spawn := func(model, slug string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{
			ToolID: "echo", ModelID: model, Slug: slug, CWD: t.TempDir(),
		})
		require.NoError(t, err)
		var ack protocol.Ack
		require.NoError(t, json.Unmarshal(env.Data, &ack))
		return ack.SessionID
	}
	finished := spawn("short", "finished")
	waiting := spawn("prompt", "waiting")

	states := func() map[string]protocol.State {
		sc, err := client.Dial(d.Socket)
		require.NoError(t, err)
		defer sc.Close()
		snap, err := sc.Hello("prune-test-poll")
		require.NoError(t, err)
		out := map[string]protocol.State{}
		for _, s := range snap.Sessions {
			out[s.ID] = s.State
		}
		return out
	}
	require.Eventually(t, func() bool {
		st := states()
		return st[finished] == protocol.StateDone && st[waiting] == protocol.StateNeedsInput
	}, 15*time.Second, 100*time.Millisecond, "echo sessions did not settle")

	out, _ := withPruneIO(t, "", false)
	require.NoError(t, RunPrune([]string{"-socket", d.Socket, "-yes"}))
	require.Contains(t, out.String(), "cleared 1")

	st := states()
	require.NotContains(t, st, finished, "the done session is deleted")
	require.Contains(t, st, waiting, "a session needing input is not on Completed")
}
