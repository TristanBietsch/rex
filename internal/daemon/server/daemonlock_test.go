package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// shortTempDir returns a temp dir short enough for a unix socket path
// (macOS caps sun_path at 104 bytes).
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-l")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func newTestServer(t *testing.T, sock, stateDir string) (*Server, error) {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	return New(Config{Socket: sock, StateDir: stateDir, Registry: reg, Store: state.NewStore()})
}

// serveUntilCleanup runs srv.Serve and returns a stop func that cancels it
// and waits for Serve to return.
func serveUntilCleanup(t *testing.T, srv *Server) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	require.Eventually(t, func() bool { return socketLive(srv.cfg.Socket) }, 2*time.Second, 10*time.Millisecond)
	stop := func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return")
		}
	}
	t.Cleanup(stop)
	return stop
}

// A second daemon on the same socket must be refused without touching the
// first one's socket.
func TestNew_RefusesWhileDaemonServesSocket(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	a, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	serveUntilCleanup(t, a)

	// Different state dir, same socket: the socket check catches it.
	_, err = newTestServer(t, sock, shortTempDir(t))
	require.ErrorIs(t, err, ErrAlreadyRunning)
	require.True(t, socketLive(sock), "first daemon's socket must survive the refused start")
}

// Two daemons must never share a state dir, even on different sockets.
func TestNew_RefusesWhileStateDirLocked(t *testing.T) {
	old := stateLockWait
	stateLockWait = 100 * time.Millisecond
	t.Cleanup(func() { stateLockWait = old })

	dir := shortTempDir(t)
	a, err := newTestServer(t, filepath.Join(dir, "a.sock"), dir)
	require.NoError(t, err)
	stopA := serveUntilCleanup(t, a)

	_, err = newTestServer(t, filepath.Join(dir, "b.sock"), dir)
	require.ErrorIs(t, err, ErrAlreadyRunning)

	// Once the first daemon is gone the state dir is free again.
	stopA()
	b, err := newTestServer(t, filepath.Join(dir, "b.sock"), dir)
	require.NoError(t, err)
	serveUntilCleanup(t, b)
}

// A socket file nobody listens on is stale and gets replaced.
func TestNew_ReplacesStaleSocket(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	_, err = os.Stat(sock)
	require.NoError(t, err, "stale socket file should remain")

	srv, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	serveUntilCleanup(t, srv)
}

// On exit a daemon must not unlink a socket path that a newer daemon has
// bound since.
func TestServe_LeavesSocketItNoLongerOwns(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	a, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	stopA := serveUntilCleanup(t, a)

	// Simulate a newer daemon taking over the path.
	require.NoError(t, os.Remove(sock))
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer l.Close()

	stopA()
	_, err = os.Stat(sock)
	require.NoError(t, err, "exiting daemon unlinked a socket it doesn't own")
}

// The common case: a daemon removes its own socket on exit.
func TestServe_RemovesOwnSocket(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	a, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	stop := serveUntilCleanup(t, a)
	stop()
	_, err = os.Stat(sock)
	require.True(t, os.IsNotExist(err), "socket should be unlinked: %v", err)
}
