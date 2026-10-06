package client_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func dialHello(t *testing.T, sock string) *client.Client {
	t.Helper()
	c, err := client.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)
	return c
}

func waitSessionAdded(t *testing.T, c *client.Client) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil {
			continue
		}
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			return sum.ID
		}
	}
	t.Fatal("timed out waiting for SessionAdded")
	return ""
}

func TestClient_NewSessionEchoCompletes(t *testing.T) {
	d := testutil.StartDaemon(t)
	defer d.Close()

	c := dialHello(t, d.Socket)
	require.NoError(t, c.NewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "cli-test", CWD: d.StateDir,
	}))

	gotDone := false
	deadline := time.Now().Add(12 * time.Second)
	for !gotDone && time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		env, err := c.NextEvent()
		require.NoError(t, err)
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var upd protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &upd))
		if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
			gotDone = true
		}
	}
	require.True(t, gotDone)
}

func TestClient_DeleteRemovesSession(t *testing.T) {
	d := testutil.StartDaemon(t)
	defer d.Close()

	c := dialHello(t, d.Socket)
	require.NoError(t, c.NewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "del-me", CWD: d.StateDir,
	}))
	id := waitSessionAdded(t, c)

	require.NoError(t, c.Delete(id))

	gotRemoved := false
	deadline := time.Now().Add(8 * time.Second)
	for !gotRemoved && time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil {
			continue
		}
		if env.Type == protocol.EventSessionRemoved {
			var rem protocol.SessionRemoved
			require.NoError(t, json.Unmarshal(env.Data, &rem))
			require.Equal(t, id, rem.SessionID)
			gotRemoved = true
		}
	}
	require.True(t, gotRemoved)
}

func TestClient_SetSessionFleet(t *testing.T) {
	d := testutil.StartDaemon(t)
	defer d.Close()

	c := dialHello(t, d.Socket)
	require.NoError(t, c.NewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "fleet-me", CWD: d.StateDir,
	}))
	id := waitSessionAdded(t, c)
	require.NoError(t, c.SetSessionFleet(id, "alpha"))

	gotFleet := false
	deadline := time.Now().Add(8 * time.Second)
	for !gotFleet && time.Now().Before(deadline) {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		env, err := c.NextEvent()
		if err != nil {
			continue
		}
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var upd protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &upd))
		if f, ok := upd.Patch["fleet"].(string); ok && f == "alpha" {
			gotFleet = true
		}
	}
	require.True(t, gotFleet)
}

func TestClient_SetMaxConcurrent(t *testing.T) {
	d := testutil.StartDaemon(t)
	defer d.Close()

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("test")
	require.NoError(t, err)
	require.NoError(t, c.SetMaxConcurrent(4))
}
