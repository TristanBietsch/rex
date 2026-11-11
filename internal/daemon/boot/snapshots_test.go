package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func snapRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600))
	return dir
}

type snapRig struct {
	sessions *state.Store
	store    *snapshot.Store
	rec      *Snapshots
	cancel   context.CancelFunc
}

func newSnapRig(t *testing.T, on bool) *snapRig {
	t.Helper()
	r := &snapRig{sessions: state.NewStore(), store: snapshot.NewStore(filepath.Join(t.TempDir(), "snapshots"))}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.rec = NewSnapshots(ctx, r.store, r.sessions)
	r.rec.SetEnabled(on)
	t.Cleanup(func() { cancel(); r.rec.Close() })
	return r
}

func (r *snapRig) add(t *testing.T, id, cwd string, st protocol.State) {
	t.Helper()
	require.NoError(t, r.sessions.Add(&state.Session{ID: id, ShortID: id, Slug: id, ToolID: "echo",
		CWD: cwd, State: st, StartedAt: time.Now().UTC()}))
}

func (r *snapRig) phases(t *testing.T, cwd string) []snapshot.Phase {
	t.Helper()
	es, err := r.store.Entries(cwd)
	require.NoError(t, err)
	var out []snapshot.Phase
	for _, e := range es {
		out = append(out, e.Phase)
	}
	return out
}

func TestSnapshots_TurnBoundariesCapture(t *testing.T) {
	cwd := snapRepo(t)
	r := newSnapRig(t, true)
	r.add(t, "s1", cwd, protocol.StateQueued)
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateNeedsInput))
	require.Eventually(t, func() bool { return len(r.phases(t, cwd)) == 2 }, 20*time.Second, 20*time.Millisecond)
	require.Equal(t, []snapshot.Phase{snapshot.PhaseStart, snapshot.PhaseEnd}, r.phases(t, cwd))

	// A second turn, then the session leaves the board.
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateDone))
	require.NoError(t, r.sessions.Remove("s1"))
	require.Eventually(t, func() bool { return len(r.phases(t, cwd)) == 5 }, 20*time.Second, 20*time.Millisecond)
	es, err := r.store.Entries(cwd)
	require.NoError(t, err)
	require.Equal(t, 2, es[2].Turn)
	require.Equal(t, snapshot.PhaseEnd, es[3].Phase)
	require.Equal(t, 5, len(es))
	require.NotZero(t, es[4].At)
}

func TestSnapshots_NoOpWithinWorking(t *testing.T) {
	cwd := snapRepo(t)
	r := newSnapRig(t, true)
	r.add(t, "s1", cwd, protocol.StateWorking) // added already working: turn 1 starts
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.Eventually(t, func() bool { return len(r.phases(t, cwd)) == 1 }, 20*time.Second, 20*time.Millisecond)
	r.rec.Close()
	require.Equal(t, []snapshot.Phase{snapshot.PhaseStart}, r.phases(t, cwd))
}

func TestSnapshots_FlagOffCreatesNothing(t *testing.T) {
	cwd := snapRepo(t)
	r := newSnapRig(t, false)
	r.add(t, "s1", cwd, protocol.StateQueued)
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateNeedsInput))
	require.NoError(t, r.sessions.Remove("s1"))
	r.rec.Close()
	_, err := os.Stat(r.store.Root())
	require.ErrorIs(t, err, os.ErrNotExist, "no shadow dir while the flag is off")
}

func TestSnapshots_AppliesSetting(t *testing.T) {
	r := newSnapRig(t, false)
	st := settings.NewStore()
	require.NoError(t, st.Set("turn_snapshots", true))
	r.rec.Applier()(st)
	require.True(t, r.rec.on.Load())
	require.NoError(t, st.Set("turn_snapshots", false))
	r.rec.Applier()(st)
	require.False(t, r.rec.on.Load())
}

func TestSnapshots_NonGitFolderIsHarmless(t *testing.T) {
	r := newSnapRig(t, true)
	dir := t.TempDir()
	r.add(t, "s1", dir, protocol.StateQueued)
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateNeedsInput))
	r.rec.Close()
	sess, ok := r.sessions.Get("s1")
	require.True(t, ok)
	require.Equal(t, protocol.StateNeedsInput, sess.Summary().State)
	_, err := os.Stat(r.store.Root())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSnapshots_FailedCaptureMarksTurnNotUndoable(t *testing.T) {
	cwd := snapRepo(t)
	r := newSnapRig(t, true)
	r.rec.timeout = time.Nanosecond // every capture is abandoned
	r.add(t, "s1", cwd, protocol.StateQueued)
	require.NoError(t, r.sessions.Transition("s1", protocol.StateWorking))
	require.NoError(t, r.sessions.Transition("s1", protocol.StateNeedsInput))
	r.rec.Close()
	sess, _ := r.sessions.Get("s1")
	require.Equal(t, protocol.StateNeedsInput, sess.Summary().State, "a failed capture never changes state")
	_, err := r.store.Undo(context.Background(), cwd, "s1", 1)
	require.ErrorIs(t, err, snapshot.ErrNotUndoable)
}

func TestTurnSnapshotsEnabled_DefaultOff(t *testing.T) {
	require.False(t, TurnSnapshotsEnabled())
}
