package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// seededStore holds two idle sessions (no PTY) with slugs alpha and beta.
func seededStore(t *testing.T) *state.Store {
	t.Helper()
	store := state.NewStore()
	for _, s := range []*state.Session{
		{ID: "aaaaaaaa-0000-4000-8000-000000000001", ShortID: "aaaa", ToolID: "echo", ModelID: "short", Slug: "alpha", CWD: "/tmp", State: protocol.StateDone},
		{ID: "bbbbbbbb-0000-4000-8000-000000000002", ShortID: "bbbb", ToolID: "echo", ModelID: "short", Slug: "beta", CWD: "/tmp", State: protocol.StateFailed},
	} {
		require.NoError(t, store.Add(s))
	}
	return store
}

// readUntil reads events until match returns true or the deadline passes.
func readUntil(t *testing.T, conn net.Conn, r *protocol.Reader, match func(protocol.Envelope) bool) protocol.Envelope {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if match(env) {
			return env
		}
	}
	t.Fatal("no matching event before deadline")
	return protocol.Envelope{}
}

func errorFor(reqID string) func(protocol.Envelope) bool {
	return func(env protocol.Envelope) bool {
		if env.Type != protocol.EventError {
			return false
		}
		var ee protocol.ErrorEvent
		return json.Unmarshal(env.Data, &ee) == nil && ee.ID == reqID
	}
}

func decodeError(t *testing.T, env protocol.Envelope) protocol.ErrorEvent {
	t.Helper()
	var ee protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ee))
	return ee
}

func TestIntentRename_RejectsSlugOfOtherSession(t *testing.T) {
	store := seededStore(t)
	sock, _, cancel := startServerWithStore(t, store)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "rn-1", protocol.Rename{
		SessionID: "bbbbbbbb-0000-4000-8000-000000000002", Slug: "alpha",
	}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("rn-1")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Equal(t, `slug "alpha" already in use by aaaa`, ee.Message)

	b, _ := store.Get("bbbbbbbb-0000-4000-8000-000000000002")
	require.Equal(t, "beta", b.Slug)

	// A free slug goes through and is confirmed with a slug patch.
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "rn-2", protocol.Rename{
		SessionID: "bbbbbbbb-0000-4000-8000-000000000002", Slug: "gamma",
	}))
	env := readUntil(t, conn, r, func(env protocol.Envelope) bool {
		if env.Type == protocol.EventError {
			t.Fatalf("unexpected error: %s", env.Data)
		}
		var u protocol.SessionUpdated
		return env.Type == protocol.EventSessionUpdated && json.Unmarshal(env.Data, &u) == nil && u.Patch["slug"] == "gamma"
	})
	require.Equal(t, protocol.EventSessionUpdated, env.Type)
}

func TestIntentRename_RejectsBadSlugAndEmpty(t *testing.T) {
	sock, _, cancel := startServerWithStore(t, seededStore(t))
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	id := "aaaaaaaa-0000-4000-8000-000000000001"
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "ws", protocol.Rename{SessionID: id, Slug: "a b"}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("ws")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Contains(t, ee.Message, "whitespace")

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "empty", protocol.Rename{SessionID: id}))
	ee = decodeError(t, readUntil(t, conn, r, errorFor("empty")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Contains(t, ee.Message, "slug or title required")

	require.NoError(t, w.WriteIntent(protocol.IntentRename, "gone", protocol.Rename{SessionID: "nope", Slug: "x"}))
	ee = decodeError(t, readUntil(t, conn, r, errorFor("gone")))
	require.Equal(t, protocol.ErrCodeUnknownSession, ee.Code)
}

func TestIntentNewSession_RejectsTakenSlug(t *testing.T) {
	store := seededStore(t)
	sock, dir, cancel := startServerWithStore(t, store)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)

	// alpha belongs to a done session: still selectable, still taken.
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "ns-1", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "alpha", CWD: dir,
	}))
	ee := decodeError(t, readUntil(t, conn, r, errorFor("ns-1")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Equal(t, `slug "alpha" already in use by aaaa`, ee.Message)
	require.Len(t, store.All(), 2, "rejected spawn adds nothing")

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "ns-2", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "has space", CWD: dir,
	}))
	ee = decodeError(t, readUntil(t, conn, r, errorFor("ns-2")))
	require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	require.Contains(t, ee.Message, "whitespace")
}
