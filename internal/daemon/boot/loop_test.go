package boot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/server"
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

func loopServerHost(t *testing.T) (*Loops, *state.Store) {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	store := state.NewStore()
	dir := t.TempDir()
	srv, err := server.New(server.Config{Socket: filepath.Join(dir, "s.sock"), StateDir: dir, Registry: reg, Store: store})
	require.NoError(t, err)
	l := NewLoops(dir, t.TempDir(), store, func() bool { return true })
	l.SetServer(srv)
	t.Cleanup(l.Close)
	return l, store
}

// The host calls reach the server and the store.
func TestLoops_HostOverTheServer(t *testing.T) {
	l, store := loopServerHost(t)
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", ToolID: "echo", State: protocol.StateWorking, StartedAt: time.Now()}))

	require.False(t, l.ReportsReason("echo"))
	require.False(t, l.ReportsReason("nope"))
	require.True(t, l.ReportsReason("claude"))

	require.ErrorContains(t, l.Send("s1", "x"), "not running")
	_, err := l.Spawn(t.Context(), loop.SpawnRequest{Tool: "nope", Model: "x", Slug: "a", CWD: t.TempDir()})
	require.Error(t, err)

	l.SetSessionLoop("s1", "loop-0000000a", 2)
	sum, _ := l.Session("s1")
	require.Equal(t, "loop-0000000a", sum.LoopID)
	require.NoError(t, l.EnsureLive(t.Context(), "s1"))

	l.Publish(protocol.LoopSummary{ID: "loop-0000000a"})
}

func TestLoops_EnabledAndApplier(t *testing.T) {
	require.False(t, LoopsEnabled(), "off by default")
	l, _ := loopServerHost(t)
	st, _, err := settings.LoadDefault()
	require.NoError(t, err)
	require.NoError(t, st.Set("loops", true))
	l.Applier()(st)
	require.True(t, l.Runner.Enabled())
	require.NoError(t, st.Set("loops", false))
	l.Applier()(st)
	require.False(t, l.Runner.Enabled())
}

// An intent while the flag is on reaches the runner and its refusals come
// back; one while it is off switches the runner off.
func TestLoops_IntentsWhenOn(t *testing.T) {
	l, _ := loopServerHost(t)
	_, err := l.Start(protocol.StartLoop{Name: "missing", CWD: t.TempDir()})
	require.Error(t, err)
	require.False(t, loop.IsOff(err))
	require.True(t, l.Runner.Enabled())
	for _, f := range []func(string) (protocol.LoopSummary, error){l.Pause, l.Resume, l.Stop} {
		_, err = f("loop-0000000a")
		require.ErrorContains(t, err, "unknown loop")
	}
	off := NewLoops(t.TempDir(), t.TempDir(), state.NewStore(), func() bool { return false })
	require.NoError(t, off.Runner.Enable())
	_, err = off.Start(protocol.StartLoop{Name: "x"})
	require.True(t, loop.IsOff(err))
	require.False(t, off.Runner.Enabled())
}
