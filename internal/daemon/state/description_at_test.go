package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestUpdateDescription_PatchHasDescriptionAt(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	patch := lastPatch(t, s)

	before := time.Now().UTC().Add(-time.Second)
	require.NoError(t, s.UpdateDescription("id1", "Running tests"))

	p := patch()
	require.Equal(t, "Running tests", p["description"])
	at, ok := p["description_at"].(time.Time)
	require.True(t, ok, "description_at is a time, got %T", p["description_at"])
	require.True(t, at.After(before))
	require.Equal(t, at, sess.DescriptionAt, "the patch carries the stored time")
	require.Equal(t, at, sess.Summary().DescriptionAt, "snapshots carry it too")
}

func TestDescriptionAt_PersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	sess := &Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", ModelID: "short", Slug: "ok",
		State: protocol.StateWorking, StartedAt: at, Description: "Running tests", DescriptionAt: at,
	}
	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, sess.ID)
	require.NoError(t, err)
	require.Equal(t, "Running tests", got.Description)
	require.True(t, at.Equal(got.DescriptionAt), "got %v", got.DescriptionAt)
}
