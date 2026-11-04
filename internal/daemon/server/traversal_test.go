package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func readError(t *testing.T, env protocol.Envelope) protocol.ErrorEvent {
	t.Helper()
	require.Equal(t, protocol.EventError, env.Type)
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	return ev
}

// TestSubscribe_RejectsTraversalAndUnknownSessions is the S-04 regression:
// Subscribe's session_id went straight into a path, so Replay with "../.."
// read any file named transcript.log, and an unknown id still got a sink.
func TestSubscribe_RejectsTraversalAndUnknownSessions(t *testing.T) {
	srv, sock, dir := newTestServer(t, state.NewStore())
	// <dir>/sessions/../.. is dir's parent; plant a transcript there.
	secret := filepath.Join(filepath.Dir(dir), "transcript.log")
	if _, err := os.Lstat(secret); err == nil {
		t.Skip("shared temp dir already has a transcript.log")
	}
	require.NoError(t, os.WriteFile(secret, []byte("SECRET"), 0o600))
	t.Cleanup(func() { os.Remove(secret) })
	serveTest(t, srv)

	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	cases := []struct{ id, code string }{
		{"../..", protocol.ErrCodeBadIntent},
		{"a/b", protocol.ErrCodeBadIntent},
		{"nope", protocol.ErrCodeUnknownSession},
	}
	for _, tc := range cases {
		require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "s-"+tc.id, protocol.Subscribe{SessionID: tc.id, Replay: true}))
		env := waitEvent(t, conn, r, func(e protocol.Envelope) bool {
			return e.Type == protocol.EventError || e.Type == protocol.EventSessionOutput
		})
		ev := readError(t, env)
		require.Equal(t, "s-"+tc.id, ev.ID)
		require.Equal(t, tc.code, ev.Code, "id %q", tc.id)
	}
	srv.outputSubsMu.RLock()
	defer srv.outputSubsMu.RUnlock()
	require.Empty(t, srv.outputSubs, "no output sink may be registered for a rejected subscribe")
}

// TestIntents_RejectTraversalIDs: every intent that takes a session_id
// rejects ids that are not a single path element.
func TestIntents_RejectTraversalIDs(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	for typ, payload := range map[string]any{
		protocol.IntentDelete:          protocol.Delete{SessionID: "../.."},
		protocol.IntentComplete:        protocol.Complete{SessionID: "../.."},
		protocol.IntentRename:          protocol.Rename{SessionID: "../..", Title: "x"},
		protocol.IntentRetire:          protocol.Retire{SessionID: "../.."},
		protocol.IntentSetSessionFleet: protocol.SetSessionFleet{SessionID: "../..", Fleet: "f"},
	} {
		require.NoError(t, w.WriteIntent(typ, typ, payload))
		ev := readError(t, waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventError }))
		require.Equal(t, typ, ev.ID)
		require.Equal(t, protocol.ErrCodeBadIntent, ev.Code, "intent %s", typ)
		require.Contains(t, ev.Message, "session_id")
	}
}
