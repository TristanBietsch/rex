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

// FocusFilter is obsolete but older clients still send it: no error back.
func TestIntentFocusFilter_AcceptedSilently(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentFocusFilter, "ff", map[string]string{"tool_id": "echo"}))
	// The next intent's error is the first event back: nothing for "ff".
	require.NoError(t, w.WriteIntent(protocol.IntentComplete, "sync", protocol.Complete{SessionID: "nope"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, "sync", env.ID)
}

// OpenSession was never implemented; it stays an unknown intent.
func TestIntentOpenSession_NotImplemented(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent("OpenSession", "o", map[string]string{"session_id": "x"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	ee := decodeError(t, env)
	require.Equal(t, "o", ee.ID)
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Equal(t, "intent not implemented", ee.Message)
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

// An effort the model doesn't offer is rejected before anything spawns, so the
// agent CLI never sees an invalid --effort flag.
func TestIntentNewSession_RejectsUnknownEffort(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "claude", ModelID: "haiku", Effort: "max", Slug: "x", CWD: "/tmp",
	}))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type)
	var ee protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ee))
	require.Contains(t, ee.Message, `effort "max" not offered`)
}
