//go:build !windows

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	creackpty "github.com/creack/pty"
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

// SwapBoards sends a swap event to each board on another build and lists
// it; Boards lists every board with the pid and build from its Hello.
func TestSwapBoardsAndBoards(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	board, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer board.Close()
	bw, br := protocol.NewWriter(board), protocol.NewReader(board)
	require.NoError(t, bw.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "v1", Client: protocol.ClientTUI, PID: 7, Build: "old"}))
	_ = board.SetReadDeadline(time.Now().Add(5 * time.Second))
	env, err := br.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	c, err := wclient.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("rex-cli")
	require.NoError(t, err)
	ctx, cancelDo := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDo()
	var boards protocol.Boards
	require.Eventually(t, func() bool {
		env, err := c.Do(ctx, protocol.IntentBoards, struct{}{})
		require.NoError(t, err)
		boards = protocol.Boards{}
		require.NoError(t, json.Unmarshal(env.Data, &boards))
		return len(boards.Boards) == 1
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, protocol.BoardInfo{PID: 7, Build: "old"}, boards.Boards[0])

	env, err = c.Do(ctx, protocol.IntentSwapBoards, protocol.SwapBoards{Build: "old"})
	require.NoError(t, err)
	boards = protocol.Boards{}
	require.NoError(t, json.Unmarshal(env.Data, &boards))
	require.Empty(t, boards.Boards, "a board already on the build is left alone")

	env, err = c.Do(ctx, protocol.IntentSwapBoards, protocol.SwapBoards{Build: "new"})
	require.NoError(t, err)
	boards = protocol.Boards{}
	require.NoError(t, json.Unmarshal(env.Data, &boards))
	require.Len(t, boards.Boards, 1)
	for {
		env, err := br.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSwap {
			break
		}
	}
}

// A handed-over agent is supervised by the new image: input reaches it
// and its output lands in the transcript; stopping it ends the session.
func TestAdoptSessionSupervisesTheChild(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-adopt")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	child := exec.Command("cat")
	ptmx, err := creackpty.Start(child)
	require.NoError(t, err)
	store := state.NewStore()
	sess := liveSession("ad1")
	require.NoError(t, store.Add(sess))
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: filepath.Join(dir, "s.sock"), StateDir: dir, Registry: reg, Store: store,
		Adopt: []AdoptSession{{Session: sess, Cols: 80, Rows: 24, Adopted: pty.Adopted{File: ptmx, PID: child.Process.Pid}}}})
	require.NoError(t, err)
	serveTest(t, srv)
	require.Eventually(t, func() bool { return srv.hasStop("ad1") }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, srv.SendRaw("ad1", []byte("hello-adopt\n"), 5*time.Second))
	require.Eventually(t, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "sessions", "ad1", "transcript.log"))
		return strings.Contains(string(b), "hello-adopt")
	}, 5*time.Second, 20*time.Millisecond)
	srv.StopSession("ad1")
}

// Refusals leave everything as it was: a live session that cannot be
// frozen yet, a new binary that fails the preflight, a restart already
// running, and a session that cannot park (other connections dropped and
// accepting again afterwards).
func TestHotRestartRefusals(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-hotr")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("s1")))
	reg, err := registry.Load("")
	require.NoError(t, err)
	preflightErr := errors.New("flag provided but not defined")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store,
		Hot: &HotConfig{Exe: "/x", Argv: func(string) []string { return nil },
			Preflight: func(context.Context, string, string) error { return preflightErr },
			Exec:      func(string, []string, []string) error { return errors.New("no") }}})
	require.NoError(t, err)
	serveTest(t, srv)
	require.Eventually(t, func() bool { return srv.getListener() != nil }, 5*time.Second, 10*time.Millisecond)

	err = srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "cannot be handed over yet")

	srv.RegisterFreeze("s1", func(context.Context) (pty.Frozen, func(), error) {
		return pty.Frozen{}, nil, errors.New("stuck")
	})
	err = srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "refused the handoff")

	preflightErr = nil
	other, err := wclient.Dial(sock)
	require.NoError(t, err)
	defer other.Close()
	_, err = other.Hello("rex-cli")
	require.NoError(t, err)
	err = srv.HotRestart(context.Background(), nil)
	require.ErrorContains(t, err, "stuck")
	c, err := wclient.Dial(sock)
	require.NoError(t, err, "accepting again")
	defer c.Close()
	_, err = c.Hello("rex-cli")
	require.NoError(t, err)

	srv.hotMu.Lock()
	err = srv.HotRestart(context.Background(), nil)
	srv.hotMu.Unlock()
	require.ErrorContains(t, err, "already running")
}
