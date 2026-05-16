package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestStore_AddAndGet(t *testing.T) {
	s := NewStore()
	sess := &Session{
		ID: "7d4f3c8a-...", ShortID: "7d4f", ToolID: "echo", ModelID: "short",
		Slug: "test", State: protocol.StateWorking, StartedAt: time.Now(),
	}
	require.NoError(t, s.Add(sess))

	got, ok := s.Get(sess.ID)
	require.True(t, ok)
	require.Equal(t, sess.ID, got.ID)

	got2, ok := s.GetByShortID(sess.ShortID)
	require.True(t, ok)
	require.Equal(t, sess.ID, got2.ID)
}

func TestStore_AddDuplicateIDFails(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1"}
	require.NoError(t, s.Add(sess))
	require.Error(t, s.Add(sess))
}

func TestStore_TransitionEmitsUpdate(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking}
	require.NoError(t, s.Add(sess))

	updates := make(chan Event, 4)
	cancel := s.Subscribe(func(e Event) { updates <- e })
	defer cancel()

	require.NoError(t, s.Transition("id1", protocol.StateDone))
	select {
	case e := <-updates:
		require.Equal(t, EventUpdated, e.Kind)
		require.Equal(t, "id1", e.SessionID)
		require.Equal(t, protocol.StateDone, *e.NewState)
	case <-time.After(time.Second):
		t.Fatal("no update emitted")
	}
}

func TestStore_Remove(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1"}
	require.NoError(t, s.Add(sess))
	require.NoError(t, s.Remove("id1"))
	_, ok := s.Get("id1")
	require.False(t, ok)
}

func TestStore_Snapshot(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Add(&Session{ID: "a", ShortID: "a", State: protocol.StateWorking}))
	require.NoError(t, s.Add(&Session{ID: "b", ShortID: "b", State: protocol.StateDone}))
	snap := s.Snapshot()
	require.Len(t, snap, 2)
}

func TestSummaryRoundtripsDescription(t *testing.T) {
	sess := &Session{
		ID:          "id-1",
		ShortID:     "abcd",
		Slug:        "test",
		State:       protocol.StateWorking,
		StartedAt:   time.Now().UTC(),
		LastEventAt: time.Now().UTC(),
		LastLine:    "raw line",
		Description: "running pnpm test",
	}
	sum := sess.Summary()
	require.Equal(t, "running pnpm test", sum.Description, "Summary should expose Description")
	back := fromSummary(sum)
	require.Equal(t, "running pnpm test", back.Description, "fromSummary should restore Description")
}

func TestUpdateDescriptionBroadcasts(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id-2", ShortID: "ef01", StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))

	var got string
	var gotKind EventKind
	s.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			if v, ok := e.Patch["description"].(string); ok {
				got = v
				gotKind = e.Kind
			}
		}
	})
	require.NoError(t, s.UpdateDescription("id-2", "rewriting webhook handlers"))
	require.Equal(t, "rewriting webhook handlers", got, "broadcast description")
	require.Equal(t, EventUpdated, gotKind, "event kind")
	require.Equal(t, "rewriting webhook handlers", sess.Description, "session field")
	require.False(t, sess.DescriptionAt.IsZero(), "DescriptionAt should be set")
}
