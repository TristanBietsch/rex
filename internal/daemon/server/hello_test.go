package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Store changes that race Hello's Snapshot must reach the client after it:
// neither lost in the gap nor sent ahead. A session the Snapshot already
// holds must not be announced again (the TUI appends SessionAdded rows).
func TestHello_EventsDuringSnapshotFollowIt(t *testing.T) {
	srvSide, cliSide := net.Pipe()
	defer cliSide.Close()
	c := newClient(srvSide)
	defer c.finish()

	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "a", ShortID: "a", Slug: "a", State: protocol.StateNeedsInput}))
	cancel := store.Subscribe(c.sendStoreEvent)
	defer cancel()

	// Before Hello, board events are not sent at all.
	require.NoError(t, store.Transition("a", protocol.StateNeedsInput))

	go c.hello("h", func() protocol.Snapshot {
		// Added just before the snapshot is read: in the Snapshot, and its
		// SessionAdded must be skipped.
		assert.NoError(t, store.Add(&state.Session{ID: "early", ShortID: "e", Slug: "early"}))
		snap := protocol.Snapshot{Sessions: store.Snapshot(), Filter: "all"}
		// Changes after the snapshot was read: must follow it.
		assert.NoError(t, store.Add(&state.Session{ID: "late", ShortID: "l", Slug: "late"}))
		assert.NoError(t, store.Transition("a", protocol.StateWorking))
		return snap
	})

	r := protocol.NewReader(cliSide)
	read := func() protocol.Envelope {
		t.Helper()
		require.NoError(t, cliSide.SetReadDeadline(time.Now().Add(2*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		return env
	}

	env := read()
	require.Equal(t, protocol.EventSnapshot, env.Type)
	require.Equal(t, "h", env.ID)
	var snap protocol.Snapshot
	require.NoError(t, json.Unmarshal(env.Data, &snap))
	require.Len(t, snap.Sessions, 2, "snapshot holds a and early")

	env = read()
	require.Equal(t, protocol.EventSessionAdded, env.Type, "SessionAdded for early must be skipped")
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(env.Data, &sum))
	require.Equal(t, "late", sum.ID)

	env = read()
	require.Equal(t, protocol.EventSessionUpdated, env.Type)
	var upd protocol.SessionUpdated
	require.NoError(t, json.Unmarshal(env.Data, &upd))
	require.Equal(t, "a", upd.SessionID)
	require.Equal(t, string(protocol.StateWorking), upd.Patch["state"])

	// Live from here on.
	require.NoError(t, store.UpdateLastLine("a", "next"))
	env = read()
	require.Equal(t, protocol.EventSessionUpdated, env.Type)
}
