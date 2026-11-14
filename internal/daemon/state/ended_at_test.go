package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func lastPatch(t *testing.T, s *Store) func() map[string]any {
	t.Helper()
	var patch map[string]any
	s.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			patch = e.Patch
		}
	})
	return func() map[string]any { return patch }
}

func TestTransition_EndedAtSetOnceClearedOnLive(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	patch := lastPatch(t, s)

	require.NoError(t, s.Transition("id1", protocol.StateNeedsInput))
	require.NotContains(t, patch(), "ended_at", "live states carry no ended_at")
	require.True(t, sess.EndedAt.IsZero())

	require.NoError(t, s.Transition("id1", protocol.StateDone))
	first := sess.EndedAt
	require.False(t, first.IsZero())
	require.Equal(t, first, patch()["ended_at"], "terminal Transition always carries ended_at")

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, s.Transition("id1", protocol.StateFailed))
	require.Equal(t, first, sess.EndedAt, "the first terminal time is kept")
	require.Equal(t, first, patch()["ended_at"])

	require.NoError(t, s.Transition("id1", protocol.StateWorking))
	require.True(t, sess.EndedAt.IsZero(), "going live again clears ended_at")
	v, ok := patch()["ended_at"]
	require.True(t, ok, "clear is sent as ended_at: nil")
	require.Nil(t, v)
}

func TestSetExit_SetsEndedAtBeforeMetaWrite(t *testing.T) {
	dir := t.TempDir()
	sess := &Session{ID: "id1", ShortID: "id1", State: protocol.StateWorking, StartedAt: time.Now().UTC()}
	sess.SetExit(protocol.StateFailed, nil)
	require.False(t, sess.EndedAt.IsZero())
	first := sess.EndedAt

	require.NoError(t, WriteMeta(dir, sess))
	got, err := LoadMeta(dir, "id1")
	require.NoError(t, err)
	require.True(t, got.EndedAt.Equal(first), "meta.json carries ended_at")

	sess.SetExit(protocol.StateDone, nil)
	require.Equal(t, first, sess.EndedAt, "a second exit keeps the first end time")
}

func TestSummaryAndFromSummary_EndedAtRoundTrip(t *testing.T) {
	end := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	sess := &Session{ID: "id1", State: protocol.StateDone, EndedAt: end}
	sum := sess.Summary()
	require.Equal(t, end, sum.EndedAt)
	require.Equal(t, end, fromSummary(sum).EndedAt)
}

func TestLoadAll_BackfillsEndedAt(t *testing.T) {
	dir := t.TempDir()
	last := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	write := func(id string, st protocol.State, ended time.Time) {
		require.NoError(t, WriteMeta(dir, &Session{
			ID: id, ShortID: id, State: st, StartedAt: last.Add(-time.Hour),
			LastEventAt: last, EndedAt: ended,
		}))
	}
	write("legacy-done", protocol.StateDone, time.Time{})
	write("was-working", protocol.StateWorking, time.Time{})
	kept := last.Add(-10 * time.Minute)
	write("has-end", protocol.StateFailed, kept)

	loaded, err := LoadAll(dir)
	require.NoError(t, err)
	byID := map[string]*Session{}
	for _, s := range loaded {
		byID[s.ID] = s
	}
	require.Equal(t, last, byID["legacy-done"].EndedAt, "terminal legacy file falls back to LastEventAt")
	require.Equal(t, protocol.StateCrashed, byID["was-working"].State)
	require.Equal(t, last, byID["was-working"].EndedAt, "a session that became crashed is backfilled")
	require.Equal(t, kept, byID["has-end"].EndedAt, "a recorded end is not overwritten")
}

func TestIsTerminal(t *testing.T) {
	for st, want := range map[protocol.State]bool{
		protocol.StateQueued: false, protocol.StateWorking: false, protocol.StateNeedsInput: false,
		protocol.StateDone: true, protocol.StateFailed: true, protocol.StateCrashed: true,
	} {
		require.Equal(t, want, isTerminal(st), st)
	}
}
