package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestStore_UpdateRenameBroadcastsSlugAndTitle(t *testing.T) {
	store := NewStore()
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "old", Title: "old title", CWD: "/tmp", State: protocol.StateWorking,
	}
	require.NoError(t, store.Add(sess))

	var got Event
	cancel := store.Subscribe(func(e Event) {
		if e.Kind == EventUpdated && e.Patch["slug"] != nil {
			got = e
		}
	})
	defer cancel()

	require.NoError(t, store.UpdateRename(sess.ID, "new-slug", "new title"))
	require.Equal(t, "new-slug", got.Patch["slug"])
	require.Equal(t, "new title", got.Patch["title"])
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
