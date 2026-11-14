//go:build !windows

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/handoff"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// hotRig is a served daemon with a fake hot restart: Preflight and Exec
// are recorded, and Exec always fails (nothing is ever exec'd). Session s1
// is live with a FreezeFunc over a pipe end.
type hotRig struct {
	srv        *Server
	dir        string
	store      *state.Store
	preflights []string
	execs      int
	onExec     func()
	thawed     int
	onFreeze   func()
}

func newHotRig(t *testing.T, lockFD int, preflight error) *hotRig {
	t.Helper()
	rig := &hotRig{store: state.NewStore()}
	require.NoError(t, rig.store.Add(liveSession("s1")))
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	ptyFD, err := rawFD(pr)
	require.NoError(t, err)
	if lockFD == 0 {
		lockFD, err = rawFD(pw)
		require.NoError(t, err)
	}
	rig.srv, rig.dir = newHotTestServer(t, rig.store, func(cfg *Config) {
		cfg.Hot = &HotConfig{
			Exe: "/nonexistent/rex-daemon", LockFD: lockFD,
			Argv: func(p string) []string { return []string{"rex-daemon", "-handoff", p} },
			Preflight: func(_ context.Context, _ string, probe string) error {
				// The probe is a handoff file this build can take over.
				require.NoError(t, handoff.Check(probe))
				rig.preflights = append(rig.preflights, probe)
				return preflight
			},
			Exec: func(string, []string, []string) error {
				rig.execs++
				if rig.onExec != nil {
					rig.onExec()
				}
				return errors.New("exec boom")
			},
		}
	})
	rig.srv.RegisterFreeze("s1", func(context.Context) (pty.Frozen, func(), error) {
		if rig.onFreeze != nil {
			rig.onFreeze()
		}
		return pty.Frozen{FD: ptyFD, PID: os.Getpid(), Cols: 80, Rows: 24}, func() { rig.thawed++ }, nil
	})
	return rig
}

// requireRolledBack checks the daemon goes on as before: no handoff file
// left, spawns allowed, and a new client is accepted.
func (rig *hotRig) requireRolledBack(t *testing.T) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(rig.dir, "handoff-*"))
	require.Empty(t, matches, "no handoff file is left behind")
	require.NoError(t, rig.srv.startGuard(), "spawns work again")
	helloClient(t, rig.srv.cfg.Socket)
}

// Over the wire: a new binary that fails the preflight refuses the restart
// before anything is frozen; the reply is restart_refused with the cause.
func TestRestartIntentPreflightRefused(t *testing.T) {
	rig := newHotRig(t, 0, errors.New("flag provided but not defined: -handoff-check"))
	c := helloClient(t, rig.srv.cfg.Socket)
	_, err := c.Do(doCtx(t), protocol.IntentRestart, protocol.Restart{Mode: protocol.RestartHot})
	we := requireErrCode(t, err, protocol.ErrCodeRestartRefused)
	require.Contains(t, we.Message, "refused the handoff")
	require.Contains(t, we.Message, "-handoff-check")
	require.Len(t, rig.preflights, 1)
	_, statErr := os.Stat(rig.preflights[0])
	require.ErrorIs(t, statErr, os.ErrNotExist, "the probe file is removed")
	require.Zero(t, rig.thawed, "nothing was frozen")
	require.Zero(t, rig.execs)
	rig.requireRolledBack(t)
}

// A session that got past the spawn guard just before the flag was set is
// running but not frozen: it would lose its PTY at exec, so the restart
// rolls back and names it.
func TestHotRestartRollsBackWhenASessionStartsDuringFreeze(t *testing.T) {
	rig := newHotRig(t, 0, nil)
	rig.onFreeze = func() { require.NoError(t, rig.store.Add(liveSession("late"))) }
	err := rig.srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "started during the restart")
	require.ErrorContains(t, err, "late")
	require.Equal(t, 1, rig.thawed, "s1 is thawed")
	require.Zero(t, rig.execs)
	rig.requireRolledBack(t)
}

// A session meta that cannot be persisted stops the restart: the new image
// would adopt a session it knows nothing about.
func TestHotRestartRollsBackOnPersistFailure(t *testing.T) {
	rig := newHotRig(t, 0, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(rig.dir, "sessions"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, "sessions", "s1"), []byte("not a dir"), 0o600))
	err := rig.srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "persist s1")
	require.Equal(t, 1, rig.thawed)
	require.Zero(t, rig.execs)
	rig.requireRolledBack(t)
}

// The handoff file is created O_EXCL: when its path cannot be claimed (here
// a non-empty directory sits there) the restart rolls back.
func TestHotRestartRollsBackWhenTheHandoffFileCannotBeWritten(t *testing.T) {
	rig := newHotRig(t, 0, nil)
	blocker := handoff.Path(rig.dir, os.Getpid())
	require.NoError(t, os.MkdirAll(filepath.Join(blocker, "x"), 0o700))
	err := rig.srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "handoff: create")
	require.Equal(t, 1, rig.thawed)
	require.Zero(t, rig.execs)
	require.NoError(t, os.RemoveAll(blocker))
	rig.requireRolledBack(t)
}

// A descriptor that cannot be marked inheritable (a bad lock fd) stops the
// restart before the exec; the written handoff file is removed again.
func TestHotRestartRollsBackWhenAnFDCannotBeInherited(t *testing.T) {
	rig := newHotRig(t, -1, nil)
	err := rig.srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "bad fd -1")
	require.Equal(t, 1, rig.thawed)
	require.Zero(t, rig.execs)
	rig.requireRolledBack(t)
}

// While the image is being replaced, a frozen session cannot be completed
// or deleted and no session can start, but a session that is not part of
// the handoff (already done) can still be completed and deleted.
func TestRestartingRefusesOnlyFrozenSessions(t *testing.T) {
	rig := newHotRig(t, 0, nil)
	done := liveSession("d1")
	done.State = protocol.StateDone
	require.NoError(t, rig.store.Add(done))
	completed := map[string]int{}
	rig.srv.RegisterComplete("s1", func() { completed["s1"]++ })
	rig.srv.RegisterComplete("d1", func() { completed["d1"]++ })
	var spawnErr, deleteFrozen, deleteDone error
	rig.onExec = func() {
		spawnErr = rig.srv.startGuard()
		rig.srv.CompleteSession("s1")
		rig.srv.CompleteSession("d1")
		deleteFrozen = rig.srv.deleteSession("s1", "test")
		deleteDone = rig.srv.deleteSession("d1", "test")
	}
	err := rig.srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "exec boom")
	require.ErrorIs(t, spawnErr, errRestarting)
	require.ErrorIs(t, deleteFrozen, errRestarting)
	require.NoError(t, deleteDone)
	require.Equal(t, map[string]int{"d1": 1}, completed, "the frozen session is not completed")
	_, ok := rig.store.Get("s1")
	require.True(t, ok, "the frozen session survives")
	_, ok = rig.store.Get("d1")
	require.False(t, ok)

	rig.srv.CompleteSession("s1")
	require.Equal(t, 1, completed["s1"], "completes again after the rollback")
	rig.requireRolledBack(t)
}
