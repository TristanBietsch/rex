package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func waitingStore(t *testing.T, st protocol.State) (*Store, *[]map[string]any, *sync.Mutex) {
	t.Helper()
	s := NewStore()
	require.NoError(t, s.Add(&Session{ID: "a", ShortID: "a", Slug: "a", State: st,
		LastEventAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}))
	var mu sync.Mutex
	var patches []map[string]any
	s.Subscribe(func(e Event) {
		if e.Kind == EventUpdated {
			mu.Lock()
			patches = append(patches, e.Patch)
			mu.Unlock()
		}
	})
	return s, &patches, &mu
}

func TestStore_SetWaiting(t *testing.T) {
	s, patches, mu := waitingStore(t, protocol.StateWorking)
	since := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)

	require.NoError(t, s.SetWaiting("a", protocol.WaitShell, since))
	require.NoError(t, s.SetWaiting("a", protocol.WaitShell, since)) // unchanged: no patch
	sess, _ := s.Get("a")
	sum := sess.Summary()
	require.Equal(t, protocol.WaitShell, sum.WaitingOn)
	require.True(t, since.Equal(sum.WaitingSince))
	require.True(t, sum.LastEventAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), "a wait is not activity")

	require.NoError(t, s.SetWaiting("a", "", time.Time{}))
	require.Empty(t, sess.Summary().WaitingOn)
	require.True(t, sess.Summary().WaitingSince.IsZero())
	require.NoError(t, s.SetWaiting("a", "", time.Time{})) // already clear

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []map[string]any{
		{"waiting_on": protocol.WaitShell, "waiting_since": since},
		{"waiting_on": nil, "waiting_since": nil},
	}, *patches)
}

func TestStore_SetWaitingRejectsBadInput(t *testing.T) {
	s, patches, mu := waitingStore(t, protocol.StateWorking)
	require.Error(t, s.SetWaiting("missing", protocol.WaitShell, time.Now()))
	require.Error(t, s.SetWaiting("a", "teleport", time.Now()))
	require.Error(t, s.SetWaiting("a", protocol.WaitShell, time.Time{}), "a wait needs a start time")
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, *patches)
}

// A wait only exists while the session is working.
func TestStore_SetWaitingIgnoredOutsideWorking(t *testing.T) {
	for _, st := range []protocol.State{protocol.StateNeedsInput, protocol.StateQueued, protocol.StateDone} {
		s, patches, mu := waitingStore(t, st)
		require.NoError(t, s.SetWaiting("a", protocol.WaitShell, time.Now()))
		sess, _ := s.Get("a")
		require.Empty(t, sess.Summary().WaitingOn, st)
		mu.Lock()
		require.Empty(t, *patches, st)
		mu.Unlock()
	}
}

func TestStore_TransitionOutOfWorkingClearsWait(t *testing.T) {
	s, patches, mu := waitingStore(t, protocol.StateWorking)
	since := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, s.SetWaiting("a", protocol.WaitWeb, since))

	require.NoError(t, s.Transition("a", protocol.StateNeedsInput))
	sess, _ := s.Get("a")
	require.Empty(t, sess.Summary().WaitingOn)
	require.True(t, sess.Summary().WaitingSince.IsZero())

	mu.Lock()
	defer mu.Unlock()
	last := (*patches)[len(*patches)-1]
	require.Equal(t, protocol.StateNeedsInput, last["state"])
	require.Contains(t, last, "waiting_on")
	require.Nil(t, last["waiting_on"])
	require.Contains(t, last, "waiting_since")
	require.Nil(t, last["waiting_since"])
}

func TestStore_TransitionWorkingKeepsWait(t *testing.T) {
	s, _, _ := waitingStore(t, protocol.StateWorking)
	require.NoError(t, s.SetWaiting("a", protocol.WaitShell, time.Now().UTC()))
	require.NoError(t, s.Transition("a", protocol.StateWorking))
	sess, _ := s.Get("a")
	require.Equal(t, protocol.WaitShell, sess.Summary().WaitingOn)
}

func TestTransitionWithoutWaitAddsNoWaitingKeys(t *testing.T) {
	s, patches, mu := waitingStore(t, protocol.StateWorking)
	require.NoError(t, s.Transition("a", protocol.StateNeedsInput))
	mu.Lock()
	defer mu.Unlock()
	require.NotContains(t, (*patches)[0], "waiting_on")
}

func TestSession_SetExitClearsWait(t *testing.T) {
	sess := &Session{ID: "a", State: protocol.StateWorking, WaitingOn: protocol.WaitShell, WaitingSince: time.Now()}
	sess.SetExit(protocol.StateDone, nil)
	require.Empty(t, sess.Summary().WaitingOn)
	require.True(t, sess.Summary().WaitingSince.IsZero())
}

// meta.json records the fields while the session works; a restart maps a
// live state to crashed, so they do not come back.
func TestMetaRoundTrip_WaitingNotRestored(t *testing.T) {
	root := t.TempDir()
	since := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)
	s := &Session{ID: "8f6a1b2c-1234-4abc-89ab-cdefabcdef01", ShortID: "8f6a", Slug: "x",
		State: protocol.StateWorking, WaitingOn: protocol.WaitShell, WaitingSince: since}
	require.NoError(t, WriteMeta(root, s))

	raw, err := os.ReadFile(filepath.Join(root, "sessions", s.ID, "meta.json"))
	require.NoError(t, err)
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(raw, &sum))
	require.Equal(t, protocol.WaitShell, sum.WaitingOn, "persisted in meta")
	require.True(t, since.Equal(sum.WaitingSince))

	loaded, err := LoadAll(root)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, protocol.StateCrashed, loaded[0].State)
	require.Empty(t, loaded[0].WaitingOn)
	require.True(t, loaded[0].WaitingSince.IsZero())
}
