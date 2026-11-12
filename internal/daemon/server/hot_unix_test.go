//go:build !windows

package server

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	wclient "github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// R20 review: an exec that fails leaves the daemon fully working: the
// session is thawed, the listener is still non-blocking and accepts again,
// and nothing could delete, complete or spawn a session while it was
// frozen.
func TestHotRestartExecFailureRollsBack(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-hot")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("s1")))
	reg, err := registry.Load("")
	require.NoError(t, err)
	lock, err := os.Create(filepath.Join(dir, "lock"))
	require.NoError(t, err)
	defer lock.Close()
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	defer pw.Close()
	ptyFD, err := rawFD(pr)
	require.NoError(t, err)
	lockFD, err := rawFD(lock)
	require.NoError(t, err)

	var srv *Server
	var duringExec []error
	srv, err = New(Config{
		Socket: sock, StateDir: dir, Registry: reg, Store: store,
		Hot: &HotConfig{
			Exe: "/nonexistent/rex-daemon", LockFD: lockFD,
			Argv:      func(p string) []string { return []string{"rex-daemon", "-handoff", p} },
			Preflight: func(context.Context, string, string) error { return nil },
			Exec: func(string, []string, []string) error {
				duringExec = append(duringExec,
					srv.deleteSession("s1", "test"),
					srv.startGuard(),
				)
				return errors.New("exec boom")
			},
		},
	})
	require.NoError(t, err)
	thawed := 0
	srv.RegisterFreeze("s1", func(context.Context) (pty.Frozen, func(), error) {
		return pty.Frozen{FD: ptyFD, PID: os.Getpid(), Cols: 80, Rows: 24}, func() { thawed++ }, nil
	})
	serveTest(t, srv)
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sock)
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)

	err = srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "exec boom")
	require.Equal(t, 1, thawed, "the session is thawed")
	require.Len(t, duringExec, 2)
	require.ErrorIs(t, duringExec[0], errRestarting, "no delete of a frozen session")
	require.ErrorIs(t, duringExec[1], errRestarting, "no spawn while restarting")
	require.NoError(t, srv.startGuard(), "spawns work again after the rollback")
	_, ok := store.Get("s1")
	require.True(t, ok)

	lfd, err := rawListenerFD(srv)
	require.NoError(t, err)
	fl, err := unix.FcntlInt(uintptr(lfd), unix.F_GETFL, 0)
	require.NoError(t, err)
	require.NotZero(t, fl&unix.O_NONBLOCK, "the listener stays non-blocking")
	fdfl, err := unix.FcntlInt(uintptr(ptyFD), unix.F_GETFD, 0)
	require.NoError(t, err)
	require.NotZero(t, fdfl&unix.FD_CLOEXEC, "close-on-exec restored")

	c, err := wclient.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("hot-test")
	require.NoError(t, err, "accepting again")
	matches, _ := filepath.Glob(filepath.Join(dir, "handoff-*"))
	require.Empty(t, matches)
}

func rawListenerFD(s *Server) (int, error) {
	rc, err := s.getListener().SyscallConn()
	if err != nil {
		return -1, err
	}
	fd := -1
	err = rc.Control(func(u uintptr) { fd = int(u) })
	return fd, err
}

// R20 review: a handed-over session that cannot be supervised is not left
// running unsupervised: its PTY is closed, its child killed and reaped, and
// the session ends failed.
func TestAdoptFailureStopsTheChild(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore()
	sess := liveSession("..")
	sess.ID = "../escape" // HookFile refuses it: adopt fails
	require.NoError(t, store.Add(sess))
	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, child.Start())
	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pw.Close()
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: filepath.Join(dir, "s"), StateDir: dir, Registry: reg, Store: store,
		Adopt: []AdoptSession{{Session: sess, Cols: 80, Rows: 24, Adopted: pty.Adopted{File: pr, PID: child.Process.Pid}}}})
	require.NoError(t, err)
	srv.startAdopted(context.Background())

	require.Eventually(t, func() bool {
		var ws syscall.WaitStatus
		pid, _ := syscall.Wait4(child.Process.Pid, &ws, syscall.WNOHANG, nil)
		return pid == child.Process.Pid || syscall.Kill(child.Process.Pid, 0) != nil
	}, 5*time.Second, 20*time.Millisecond, "the child is killed")
	_, err = pr.Stat()
	require.Error(t, err, "the PTY file is closed")
	got, ok := store.Get("../escape")
	require.True(t, ok)
	require.Equal(t, protocol.StateFailed, got.Summary().State)
}
