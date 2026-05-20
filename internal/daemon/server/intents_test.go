package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func helloOnConn(t *testing.T, conn net.Conn) (*protocol.Writer, *protocol.Reader) {
	t.Helper()
	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "test"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)
	return w, r
}

func TestIntentSetMaxConcurrent_Accepted(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, _ := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentSetMaxConcurrent, "", protocol.SetMaxConcurrent{N: 2}))
}

func TestIntentFocusFilter_AcceptedSilently(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, _ := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentFocusFilter, "", protocol.FocusFilter{ToolID: "echo"}))
}

func TestIntentRename_TriggersSessionUpdated(t *testing.T) {
	sock, dir, cancel := startServer(t)
	defer cancel()

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "rename-me", CWD: dir,
	}))

	var sessID string
	deadline := time.Now().Add(5 * time.Second)
	for sessID == "" && time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			sessID = sum.ID
		}
	}
	require.NotEmpty(t, sessID)

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "", protocol.Rename{
		SessionID: sessID, Slug: "renamed-slug",
	}))

	gotUpdate := false
	for !gotUpdate && time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var upd protocol.SessionUpdated
		require.NoError(t, json.Unmarshal(env.Data, &upd))
		if upd.SessionID == sessID {
			if slug, ok := upd.Patch["slug"].(string); ok && slug == "renamed-slug" {
				gotUpdate = true
			}
		}
	}
	require.True(t, gotUpdate)
}

func TestIntentNewSession_RejectsMissingFields(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "", CWD: "/tmp",
	}))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type)
}
