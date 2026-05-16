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
		ID:            "id-1",
		ShortID:       "abcd",
		Slug:          "test",
		State:         protocol.StateWorking,
		StartedAt:     time.Now().UTC(),
		LastEventAt:   time.Now().UTC(),
		LastLine:      "raw line",
		Description:   "running pnpm test",
		DescriptionAt: time.Now().UTC(),
	}
	sum := sess.Summary()
	if sum.Description != "running pnpm test" {
		t.Fatalf("Summary.Description: got %q want %q", sum.Description, "running pnpm test")
	}
	back := fromSummary(sum)
	if back.Description != "running pnpm test" {
		t.Fatalf("fromSummary.Description: got %q want %q", back.Description, "running pnpm test")
	}
}
