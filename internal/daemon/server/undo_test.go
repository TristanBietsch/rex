package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type undoEnv struct {
	*retireEnv
	snaps *snapshot.Store
	on    *bool
}

func undoRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600))
	return dir
}

func startUndoServer(t *testing.T, on bool, seed ...*state.Session) *undoEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-undo")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store := state.NewStore()
	for _, s := range seed {
		require.NoError(t, store.Add(s))
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	e := &undoEnv{snaps: snapshot.NewStore(filepath.Join(dir, "snapshots")), on: &on}
	sock := filepath.Join(dir, "rex.sock")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store,
		Snapshots: e.snaps, SnapshotsEnabled: func() bool { return *e.on }})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		cancel()
		<-served
	})
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond)
	e.retireEnv = &retireEnv{srv: srv, store: store, sock: sock, dir: dir}
	return e
}

func undoSession(id, cwd string, st protocol.State) *state.Session {
	return &state.Session{ID: id, ShortID: id, Slug: id, ToolID: "echo", CWD: cwd, State: st, StartedAt: time.Now().UTC()}
}

// undoCall sends an Undo or Redo and returns the UndoResult, or the Error.
func undoCall(t *testing.T, e *undoEnv, intent string, payload any) (res protocol.UndoResult, code, msg string) {
	t.Helper()
	conn, w, r := e.dial(t)
	reqID := "req-undo"
	require.NoError(t, w.WriteIntent(intent, reqID, payload))
	env := waitEvent(t, conn, r, func(env protocol.Envelope) bool {
		return env.ID == reqID && (env.Type == protocol.EventUndoResult || env.Type == protocol.EventError || env.Type == protocol.EventAck)
	})
	switch env.Type {
	case protocol.EventUndoResult:
		require.NoError(t, json.Unmarshal(env.Data, &res))
	case protocol.EventError:
		var ev protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &ev))
		return res, ev.Code, ev.Message
	default:
		t.Fatalf("plain Ack for %s", intent)
	}
	return res, "", ""
}

func agentTurn(t *testing.T, e *undoEnv, repo, session, file, content string) {
	t.Helper()
	ctx := context.Background()
	_, err := e.snaps.Capture(ctx, repo, session, snapshot.PhaseStart)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(repo, file), []byte(content), 0o600))
	_, err = e.snaps.Capture(ctx, repo, session, snapshot.PhaseEnd)
	require.NoError(t, err)
}

func TestUndo_RefusedWhileFlagOff(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, false, undoSession("a", repo, protocol.StateNeedsInput))
	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	require.Contains(t, msg, "off")
	_, code, _ = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	_, err := os.Stat(e.snaps.Root())
	require.ErrorIs(t, err, os.ErrNotExist, "no shadow dir while off")
}

func TestUndo_RefusedWhileWorking(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateWorking))
	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "working")
}

func TestUndo_RefusedWithOtherLiveSessionUnlessForce(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true,
		undoSession("a", repo, protocol.StateNeedsInput),
		undoSession("b", repo, protocol.StateWorking),
		undoSession("gone", repo, protocol.StateDone),
		undoSession("elsewhere", t.TempDir(), protocol.StateWorking))
	agentTurn(t, e, repo, "a", "a.txt", "two\n")

	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "(b)", "names the other session")
	require.NotContains(t, msg, "gone")
	require.NotContains(t, msg, "elsewhere")
	got, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	require.Equal(t, "two\n", string(got), "a refused undo changes nothing")

	res, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", Force: true})
	require.Empty(t, code, msg)
	require.Equal(t, []string{"a.txt"}, res.Restored)
	got, _ = os.ReadFile(filepath.Join(repo, "a.txt"))
	require.Equal(t, "one\n", string(got))
}

func TestUndo_RedoWorksThenDropsAfterNewTurn(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateNeedsInput))
	agentTurn(t, e, repo, "a", "a.txt", "two\n")
	agentTurn(t, e, repo, "a", "new.txt", "created\n")

	res, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Empty(t, code, msg)
	require.Equal(t, "a", res.SessionID)
	require.Equal(t, []string{"new.txt"}, res.Deleted)
	require.NotEmpty(t, res.Safety)
	require.Equal(t, []string{}, res.Restored)
	_, err := os.Stat(filepath.Join(repo, "new.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)

	res, code, msg = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a"})
	require.Empty(t, code, msg)
	require.Equal(t, []string{"new.txt"}, res.Restored)
	got, _ := os.ReadFile(filepath.Join(repo, "new.txt"))
	require.Equal(t, "created\n", string(got))

	_, code, _ = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code, "nothing left to redo")

	// Undo, then a new turn: redo history is dropped.
	_, code, msg = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a"})
	require.Empty(t, code, msg)
	agentTurn(t, e, repo, "a", "a.txt", "three\n")
	_, code, msg = undoCall(t, e, protocol.IntentRedo, protocol.Redo{SessionID: "a"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "nothing to redo")
}

func TestUndo_MultipleTurns(t *testing.T) {
	repo := undoRepo(t)
	e := startUndoServer(t, true, undoSession("a", repo, protocol.StateNeedsInput))
	agentTurn(t, e, repo, "a", "a.txt", "t1\n")
	agentTurn(t, e, repo, "a", "a.txt", "t2\n")
	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "a", Turns: 2})
	require.Empty(t, code, msg)
	got, _ := os.ReadFile(filepath.Join(repo, "a.txt"))
	require.Equal(t, "one\n", string(got))
}

func TestUndo_BadRequests(t *testing.T) {
	plain := t.TempDir()
	repo := undoRepo(t)
	e := startUndoServer(t, true,
		undoSession("nogit", plain, protocol.StateNeedsInput),
		undoSession("fresh", repo, protocol.StateNeedsInput))

	_, code, msg := undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "nogit"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "git")

	_, code, _ = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "missing"})
	require.Equal(t, protocol.ErrCodeUnknownSession, code)

	_, code, _ = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "fresh", Turns: -1})
	require.Equal(t, protocol.ErrCodeBadIntent, code)
	_, code, _ = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "bad id/../"})
	require.Equal(t, protocol.ErrCodeBadIntent, code)

	_, code, msg = undoCall(t, e, protocol.IntentUndo, protocol.Undo{SessionID: "fresh"})
	require.Equal(t, protocol.ErrCodeBadState, code)
	require.Contains(t, msg, "nothing to undo")
}
