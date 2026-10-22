package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func addSession(t *testing.T, s *Store, id string, st protocol.State) *Session {
	t.Helper()
	sess := &Session{ID: id, ShortID: id, Slug: id, State: st, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	return sess
}

func collect(s *Store) (func() []Event, func()) {
	var mu sync.Mutex
	var got []Event
	cancel := s.Subscribe(func(e Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})
	return func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), got...)
	}, cancel
}

func TestStore_SetRetire_BroadcastsOnce_KeepsOriginalDeadline(t *testing.T) {
	s := NewStore()
	addSession(t, s, "a", protocol.StateNeedsInput)
	events, cancel := collect(s)
	defer cancel()

	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	changed, err := s.SetRetire("a", first)
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = s.SetRetire("a", first.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, changed)

	sess, _ := s.Get("a")
	require.True(t, first.Equal(sess.Summary().RetireAt))
	require.Empty(t, events(), "callers persist then broadcast")
}

func TestStore_ClearRetire(t *testing.T) {
	s := NewStore()
	sess := addSession(t, s, "a", protocol.StateDone)
	_, _ = s.SetRetire("a", time.Now().Add(time.Hour))

	changed, err := s.ClearRetire("a")
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, sess.Summary().RetireAt.IsZero())

	changed, err = s.ClearRetire("a")
	require.NoError(t, err)
	require.False(t, changed)
}

func TestStore_ClaimExpired_BoundaryOnceAndBusyWaits(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	addSession(t, s, "due", protocol.StateDone)
	addSession(t, s, "future", protocol.StateDone)
	addSession(t, s, "busy", protocol.StateNeedsInput)
	addSession(t, s, "unmarked", protocol.StateDone)
	_, _ = s.SetRetire("due", now) // deadline == now counts as expired
	_, _ = s.SetRetire("future", now.Add(time.Second))
	_, _ = s.SetRetire("busy", now.Add(-time.Hour))
	require.NoError(t, s.Transition("busy", protocol.StateWorking))

	claimed, waiting := s.ClaimExpired(now)
	require.Len(t, claimed, 1)
	require.Equal(t, "due", claimed[0].ID)
	require.Len(t, waiting, 1)
	require.Equal(t, "busy", waiting[0].ID)

	claimed, _ = s.ClaimExpired(now)
	require.Empty(t, claimed, "a session is claimed at most once")

	// Busy session settles → claimable.
	require.NoError(t, s.Transition("busy", protocol.StateNeedsInput))
	claimed, waiting = s.ClaimExpired(now)
	require.Len(t, claimed, 1)
	require.Equal(t, "busy", claimed[0].ID)
	require.Empty(t, waiting)
}

func TestStore_ClearRetireAfterClaim_ErrReaping(t *testing.T) {
	s := NewStore()
	now := time.Now()
	addSession(t, s, "a", protocol.StateDone)
	_, _ = s.SetRetire("a", now.Add(-time.Second))
	claimed, _ := s.ClaimExpired(now)
	require.Len(t, claimed, 1)
	_, err := s.ClearRetire("a")
	require.ErrorIs(t, err, ErrReaping)
}

func TestStore_ClaimVsClear_ExactlyOneWins(t *testing.T) {
	for i := 0; i < 200; i++ {
		s := NewStore()
		now := time.Now()
		addSession(t, s, "a", protocol.StateDone)
		_, _ = s.SetRetire("a", now.Add(-time.Second))
		var claimedN, clearedN atomic.Int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			c, _ := s.ClaimExpired(now)
			claimedN.Add(int32(len(c)))
		}()
		go func() {
			defer wg.Done()
			if changed, err := s.ClearRetire("a"); err == nil && changed {
				clearedN.Add(1)
			}
		}()
		wg.Wait()
		require.Equal(t, int32(1), claimedN.Load()+clearedN.Load(), "iteration %d", i)
	}
}

func TestStore_Transition_TracksWorkingSince(t *testing.T) {
	s := NewStore()
	addSession(t, s, "a", protocol.StateQueued)
	events, cancel := collect(s)
	defer cancel()

	require.NoError(t, s.Transition("a", protocol.StateWorking))
	sess, _ := s.Get("a")
	since := sess.Summary().WorkingSince
	require.False(t, since.IsZero())
	require.Contains(t, events()[0].Patch, "working_since")

	// working → working keeps the original start, no working_since patch.
	require.NoError(t, s.Transition("a", protocol.StateWorking))
	require.True(t, since.Equal(sess.Summary().WorkingSince))
	require.NotContains(t, events()[1].Patch, "working_since")

	require.NoError(t, s.Transition("a", protocol.StateNeedsInput))
	require.True(t, sess.Summary().WorkingSince.IsZero())
	v, ok := events()[2].Patch["working_since"]
	require.True(t, ok)
	require.Nil(t, v)
}

func TestStore_SetName_NoActivityBump(t *testing.T) {
	s := NewStore()
	sess := addSession(t, s, "a", protocol.StateDone)
	before := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	sess.LastEventAt = before
	events, cancel := collect(s)
	defer cancel()

	changed, err := s.SetName("a", "my task")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "my task", sess.Summary().Name)
	require.True(t, before.Equal(sess.Summary().LastEventAt))
	require.Empty(t, events())

	changed, err = s.SetName("a", "my task")
	require.NoError(t, err)
	require.False(t, changed)

	_, err = s.SetName("missing", "x")
	require.Error(t, err)
}

func TestRemoveSessionDir_RejectsUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "sessions", "keep")
	require.NoError(t, os.MkdirAll(keep, 0o755))
	for _, id := range []string{"", ".", "..", "../x", "a/b", "/abs", "a/../.."} {
		require.ErrorIs(t, RemoveSessionDir(root, id), ErrUnsafeSessionID, "id %q", id)
	}
	_, err := os.Stat(keep)
	require.NoError(t, err, "sibling session dirs must survive")
}

func TestLoadAll_SkipsIDDirMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "real")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	b, _ := json.Marshal(protocol.SessionSummary{ID: "", Slug: "evil"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644))

	got, err := LoadAll(root)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestMeta_RoundTripNameRetireAt_CrashRemapClearsWorkingSince(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	sess := &Session{ID: "a", ShortID: "a", State: protocol.StateWorking, Name: "label",
		RetireAt: at, WorkingSince: at.Add(-time.Hour)}
	require.NoError(t, WriteMeta(root, sess))

	all, err := LoadAll(root)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "label", all[0].Name)
	require.True(t, at.Equal(all[0].RetireAt))
	require.Equal(t, protocol.StateCrashed, all[0].State)
	require.True(t, all[0].WorkingSince.IsZero())
}

func TestWriteMeta_AfterRemove_NoResurrect(t *testing.T) {
	root := t.TempDir()
	s := NewStore()
	sess := addSession(t, s, "a", protocol.StateDone)
	require.NoError(t, WriteMeta(root, sess))
	require.NoError(t, s.Remove("a"))
	require.NoError(t, RemoveSession(root, sess))

	require.NoError(t, WriteMeta(root, sess))
	_, err := os.Stat(filepath.Join(root, "sessions", "a"))
	require.True(t, os.IsNotExist(err), "late WriteMeta recreated a deleted session dir")
}

func TestWriteMeta_ConcurrentWritersLeaveLatest(t *testing.T) {
	root := t.TempDir()
	sess := &Session{ID: "a", ShortID: "a"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess.mu.Lock()
			sess.Name = fmt.Sprintf("n%d", i)
			sess.mu.Unlock()
			require.NoError(t, WriteMeta(root, sess))
		}(i)
	}
	wg.Wait()
	got, err := LoadMeta(root, "a")
	require.NoError(t, err)
	require.Equal(t, sess.Summary().Name, got.Name)
	entries, _ := os.ReadDir(filepath.Join(root, "sessions", "a"))
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp", "temp files must not leak")
	}
}
