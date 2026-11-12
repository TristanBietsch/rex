package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	l := NewLoops(context.Background(), dir, filepath.Join(dir, "files"), store, func() bool { return false })
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
	l := NewLoops(context.Background(), t.TempDir(), t.TempDir(), state.NewStore(), func() bool { return true })
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
	l := NewLoops(context.Background(), t.TempDir(), t.TempDir(), store, func() bool { return true })
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
	l := NewLoops(context.Background(), dir, t.TempDir(), store, func() bool { return true })
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

func loopServerHost(t *testing.T) (*Loops, *state.Store, *server.Server) {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	store := state.NewStore()
	dir := t.TempDir()
	srv, err := server.New(server.Config{Socket: filepath.Join(dir, "s.sock"), StateDir: dir, Registry: reg, Store: store})
	require.NoError(t, err)
	l := NewLoops(context.Background(), dir, t.TempDir(), store, func() bool { return true })
	l.SetServer(srv)
	t.Cleanup(l.Close)
	return l, store, srv
}

// The host calls reach the server and the store.
func TestLoops_HostOverTheServer(t *testing.T) {
	l, store, _ := loopServerHost(t)
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", ToolID: "echo", State: protocol.StateWorking, StartedAt: time.Now()}))

	require.False(t, l.ReportsReason("echo"))
	require.False(t, l.ReportsReason("nope"))
	require.True(t, l.ReportsReason("claude"))

	_, err := l.Send("s1", "x", nil)
	require.ErrorContains(t, err, "not running")
	_, err = l.Spawn(t.Context(), loop.SpawnRequest{Tool: "nope", Model: "x", Slug: "a", CWD: t.TempDir()})
	require.Error(t, err)

	l.SetSessionLoop("s1", "loop-0000000a", 2)
	sum, _ := l.Session("s1")
	require.Equal(t, "loop-0000000a", sum.LoopID)
	require.NoError(t, l.EnsureLive(t.Context(), "s1"))

	l.Publish(protocol.LoopSummary{ID: "loop-0000000a"})
}

func TestLoops_EnabledAndApplier(t *testing.T) {
	l, _, _ := loopServerHost(t)
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
	l, _, _ := loopServerHost(t)
	_, err := l.Start(protocol.StartLoop{Name: "missing", CWD: t.TempDir()})
	require.Error(t, err)
	require.False(t, loop.IsOff(err))
	require.True(t, l.Runner.Enabled())
	for _, f := range []func(string) (protocol.LoopSummary, error){l.Pause, l.Resume, l.Stop} {
		_, err = f("loop-0000000a")
		require.ErrorContains(t, err, "unknown loop")
	}
	off := NewLoops(context.Background(), t.TempDir(), t.TempDir(), state.NewStore(), func() bool { return false })
	require.NoError(t, off.Runner.Enable())
	_, err = off.Start(protocol.StartLoop{Name: "x"})
	require.True(t, loop.IsOff(err))
	require.False(t, off.Runner.Enabled())
}

// Only a tool whose adapter says why it waits may run loops: Claude hooks and
// OSC 7501 status do; a plain heuristic tool does not.
func TestToolReportsReason(t *testing.T) {
	require.True(t, toolReportsReason(registry.Tool{Detect: registry.Detect{Kind: "hooks", Format: "claude"}}))
	require.False(t, toolReportsReason(registry.Tool{Detect: registry.Detect{Kind: "heuristic"}}))
	require.True(t, toolReportsReason(registry.Tool{Detect: registry.Detect{Kind: "heuristic", Status: registry.StatusOSC7501}}))
	require.False(t, toolReportsReason(registry.Tool{Detect: registry.Detect{Kind: "hooks", Format: "other"}}))
}

// realLoop puts a paused loop over a real store session (claude, at the given
// wait reason) and a real input channel, so a Resume goes through the runner,
// the real Server.SendText and its lock.
func realLoop(t *testing.T, reason, lastLine string) (l *Loops, id string, input chan []byte) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the check is the Unix false command")
	}
	l, store, srv := loopServerHost(t)
	require.NoError(t, store.Add(&state.Session{ID: "s1", ShortID: "s1", Slug: "s", ToolID: "claude",
		State: protocol.StateNeedsInput, LastLine: lastLine, StartedAt: time.Now()}))
	require.NoError(t, store.SetWaitReason("s1", reason))
	input = make(chan []byte, 8)
	srv.RegisterInputChannel("s1", input)

	def, err := loop.Parse([]byte("name: fix\nkind: repeat\ntool: claude\nmodel: sonnet\nprompt: \"first\"\nfollowup_prompt: \"go on\"\n"+
		"until:\n  check: [\"false\"]\n"), "fix")
	require.NoError(t, err)
	def.CWD = t.TempDir()
	id = "loop-0000000a"
	st := loop.State{Version: 1, ID: id, Def: def, SessionID: "s1", State: protocol.LoopPaused, PauseReason: protocol.PauseUser,
		Iteration: 1, StartedAt: time.Now(), UpdatedAt: time.Now()}
	b, err := json.Marshal(st)
	require.NoError(t, err)
	dir := filepath.Join(l.stateDir, "loops")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".json"), b, 0o600))
	return l, id, input
}

// At a real turn end the runner types the follow-up and then Enter, as two
// writes to the session's input, and nothing else.
func TestLoops_RealSendAtATurnEnd(t *testing.T) {
	l, id, input := realLoop(t, protocol.WaitReview, "all fixed.")
	got, err := l.Resume(id)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopRunning, got.State)
	select {
	case b := <-input:
		require.Equal(t, []byte("go on"), b)
	case <-time.After(20 * time.Second):
		t.Fatal("the follow-up was never typed")
	}
	select {
	case b := <-input:
		require.Equal(t, []byte{'\r'}, b)
	case <-time.After(5 * time.Second):
		t.Fatal("no Enter after the text")
	}
	time.Sleep(200 * time.Millisecond)
	require.Empty(t, input, "nothing else was written")
}

// At a permission prompt it writes nothing.
func TestLoops_RealPromptGetsNoBytes(t *testing.T) {
	l, id, input := realLoop(t, protocol.WaitInput, "Allow Bash(rm -rf /)?")
	got, err := l.Resume(id)
	require.NoError(t, err)
	require.Equal(t, protocol.LoopPaused, got.State)
	require.Equal(t, protocol.PauseNeedsInput, got.PauseReason)
	time.Sleep(500 * time.Millisecond)
	require.Empty(t, input, "never type into a prompt")
}
