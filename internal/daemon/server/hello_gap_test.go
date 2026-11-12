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

// A store change between Hello's Snapshot read and the client going live
// reached neither the Snapshot nor the client: the board stayed stale until
// the next change to that session. Board events are now held from the
// start of Hello and sent right after the Snapshot.
func TestHello_ChangeDuringSnapshotIsNotLost(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "before", State: protocol.StateWorking, StartedAt: time.Now().UTC()}))
	srv, sock, _ := newTestServer(t, store)
	prev := helloSnapshotTaken
	helloSnapshotTaken = func() { _ = store.UpdateRename("s1", "after", "") }
	t.Cleanup(func() { helloSnapshotTaken = prev })
	serveTest(t, srv)

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	_, r := helloOnConn(t, conn)
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		env, err := r.Read()
		require.NoError(t, err, "the rename made during the snapshot never arrived")
		if env.Type != protocol.EventSessionUpdated {
			continue
		}
		var patch map[string]any
		require.NoError(t, json.Unmarshal(env.Data, &patch))
		if p, ok := patch["patch"].(map[string]any); ok && p["slug"] == "after" {
			return
		}
		if patch["slug"] == "after" {
			return
		}
	}
}
