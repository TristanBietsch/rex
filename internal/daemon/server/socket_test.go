package server

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestServe_RefusesSocketOfLiveDaemon is the P0-03 regression: a second
// daemon used to unlink the first one's socket and listen in its place.
func TestServe_RefusesSocketOfLiveDaemon(t *testing.T) {
	first, sock, dir := newTestServer(t, state.NewStore())
	serveTest(t, first)

	second, err := New(Config{Socket: sock, StateDir: dir, Registry: first.Registry(), Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- second.Serve(ctx) }()
	select {
	case err := <-served:
		require.ErrorIs(t, err, ErrSocketInUse)
		require.ErrorContains(t, err, sock)
	case <-time.After(3 * time.Second):
		cancel()
		<-served
		t.Fatal("second Serve started on a socket a live daemon answers")
	}

	// The first daemon still owns the path.
	conn := dialTest(t, sock)
	helloOnConn(t, conn)
	require.Equal(t, 1, first.liveConns())
}

// TestServe_ExitLeavesReplacedSocket: a daemon exiting must not unlink a
// socket path that now belongs to someone else.
func TestServe_ExitLeavesReplacedSocket(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	cancel, served := serveTest(t, srv)

	require.NoError(t, os.Remove(sock))
	other, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { other.Close() })

	cancel()
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	_, err = os.Lstat(sock)
	require.NoError(t, err, "the replacement socket was unlinked")
	c, err := net.Dial("unix", sock)
	require.NoError(t, err)
	c.Close()
}

func TestServe_RemovesOwnSocketOnExit(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	cancel, served := serveTest(t, srv)
	cancel()
	<-served
	_, err := os.Lstat(sock)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestServe_ReplacesStaleSocket(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	// A socket file nobody listens on, as a crashed daemon leaves behind.
	l, err := net.Listen("unix", sock)
	require.NoError(t, err)
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, l.Close())
	_, err = os.Lstat(sock)
	require.NoError(t, err)

	serveTest(t, srv)
	helloOnConn(t, dialTest(t, sock))
}

func TestServe_RefusesNonSocketPath(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	require.NoError(t, os.WriteFile(sock, []byte("precious"), 0o600))
	err := srv.Serve(context.Background())
	require.ErrorContains(t, err, "not a socket")
	b, err := os.ReadFile(sock)
	require.NoError(t, err)
	require.Equal(t, "precious", string(b), "a non-socket file at the path must survive")
}

// TestServe_SocketMatchesProtocolAfterRefusal: the refused second Serve did
// not disturb the first daemon's connections.
func TestServe_SocketMatchesProtocolAfterRefusal(t *testing.T) {
	first, sock, dir := newTestServer(t, state.NewStore())
	serveTest(t, first)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)

	second, err := New(Config{Socket: sock, StateDir: dir, Registry: first.Registry(), Store: state.NewStore()})
	require.NoError(t, err)
	require.ErrorIs(t, second.Serve(context.Background()), ErrSocketInUse)

	require.NoError(t, w.WriteIntent(protocol.IntentShutdown, "s", struct{}{}))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type, "the first daemon still answers on its connection")
}
