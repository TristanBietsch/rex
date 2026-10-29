package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// UpdateRename and SetFleet only set fields: the server writes meta.json
// first and then broadcasts, so a client reacting to the event finds the
// change on disk.
func TestStore_UpdateRenameAndSetFleetDoNotBroadcast(t *testing.T) {
	store := NewStore()
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "old", Title: "old title", CWD: "/tmp", State: protocol.StateWorking,
	}
	require.NoError(t, store.Add(sess))

	var events int
	cancel := store.Subscribe(func(Event) { events++ })
	defer cancel()

	require.NoError(t, store.UpdateRename(sess.ID, "new-slug", "new title"))
	require.NoError(t, store.SetFleet(sess.ID, "backend"))
	sum := sess.Summary()
	require.Equal(t, "new-slug", sum.Slug)
	require.Equal(t, "new title", sum.Title)
	require.Equal(t, "backend", sum.Fleet)
	require.Zero(t, events)

	// Empty fields are left alone.
	require.NoError(t, store.UpdateRename(sess.ID, "", "t2"))
	require.Equal(t, "new-slug", sess.Summary().Slug)
}

func TestStore_UpdateLastLineBroadcastsPatch(t *testing.T) {
	store := NewStore()
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "s", CWD: "/tmp", State: protocol.StateWorking,
	}
	require.NoError(t, store.Add(sess))

	var got Event
	cancel := store.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			got = e
		}
	})
	defer cancel()

	require.NoError(t, store.UpdateLastLine(sess.ID, "last output line"))
	require.Equal(t, sess.ID, got.SessionID)
	require.Equal(t, "last output line", got.Patch["last_line"])
}

func TestStore_SetFleetUpdatesField(t *testing.T) {
	store := NewStore()
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "s", CWD: "/tmp", State: protocol.StateWorking,
	}
	require.NoError(t, store.Add(sess))
	require.NoError(t, store.SetFleet(sess.ID, "backend"))

	s, ok := store.Get(sess.ID)
	require.True(t, ok)
	require.Equal(t, "backend", s.Fleet)
}

func TestStore_UpdateTokensAndBroadcastPatch(t *testing.T) {
	store := NewStore()
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "s", CWD: "/tmp", State: protocol.StateWorking,
	}
	require.NoError(t, store.Add(sess))

	tokens, bytes, err := store.UpdateTokens(sess.ID, 400)
	require.NoError(t, err)
	require.Equal(t, int64(100), tokens)
	require.Equal(t, int64(400), bytes)

	var patch map[string]any
	cancel := store.Subscribe(func(e Event) {
		if e.Kind == EventUpdated && e.Patch["tokens"] != nil {
			patch = e.Patch
		}
	})
	defer cancel()

	store.BroadcastTokenPatch(sess.ID, tokens, bytes)
	require.Equal(t, int64(100), patch["tokens"])
	require.Equal(t, int64(400), patch["output_bytes"])
}
