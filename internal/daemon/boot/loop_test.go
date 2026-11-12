package boot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/loop"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Off is inert: no intent is served, no state dir appears, nothing subscribes.
func TestLoops_OffIsInert(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	l := NewLoops(dir, filepath.Join(dir, "files"), store, func() bool { return false })
	defer l.Close()
	_, err := l.Start(protocol.StartLoop{Name: "x", CWD: dir})
	require.True(t, loop.IsOff(err))
	for _, f := range []func(string) (protocol.LoopSummary, error){l.Pause, l.Resume, l.Stop} {
		_, err = f("loop-00000001")
		require.True(t, loop.IsOff(err))
	}
	require.Empty(t, l.Snapshot())
	l.UserInput("s")
	require.False(t, l.Runner.Enabled())
	require.NoDirExists(t, filepath.Join(dir, "loops"))
}

func TestLoops_ApplierFollowsTheSetting(t *testing.T) {
	l := NewLoops(t.TempDir(), t.TempDir(), state.NewStore(), func() bool { return true })
	defer l.Close()
	l.SetEnabled(true)
	require.True(t, l.Runner.Enabled())
	l.SetEnabled(false)
	require.False(t, l.Runner.Enabled())
}

// The host turns store events into runner events.
func TestLoops_SubscribeTranslatesStoreEvents(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateWorking, StartedAt: time.Now()}))
	l := NewLoops(t.TempDir(), t.TempDir(), store, func() bool { return true })
	defer l.Close()
	got := make(chan loop.Event, 8)
	cancel := l.Subscribe(func(e loop.Event) { got <- e })
	defer cancel()

	require.NoError(t, store.Transition("s1", protocol.StateNeedsInput))
	require.Equal(t, loop.Event{SessionID: "s1", State: protocol.StateNeedsInput}, <-got)
	require.NoError(t, store.SetWaitReason("s1", protocol.WaitReview))
	e := <-got
	require.NotNil(t, e.WaitReason)
	require.Equal(t, protocol.WaitReview, *e.WaitReason)
	require.NoError(t, store.SetWaitReason("s1", ""))
	e = <-got
	require.Equal(t, "", *e.WaitReason)
	require.NoError(t, store.SetLoop("s1", "loop-00000001", 2)) // not a loop event
	require.NoError(t, store.Remove("s1"))
	require.Equal(t, loop.Event{SessionID: "s1", Removed: true}, <-got)

	session, ok := l.Session("none")
	require.False(t, ok)
	require.Empty(t, session.ID)
}

func TestLoops_OutputFallsBackToTheTranscript(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", State: protocol.StateWorking, StartedAt: time.Now()}))
	l := NewLoops(dir, t.TempDir(), store, func() bool { return true })
	defer l.Close()
	store.SetScreen("s1", "on screen")
	require.Equal(t, "on screen", l.Output("s1"))
	store.SetScreen("s1", "")
	sdir, err := state.SessionDir(dir, "s1")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(sdir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sdir, "transcript.log"), []byte("from log\r\n"), 0o600))
	require.Contains(t, l.Output("s1"), "from log")
}
