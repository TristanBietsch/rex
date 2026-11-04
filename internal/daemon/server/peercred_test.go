package server

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// unixPair returns both ends of a real unix socket connection.
func unixPair(t *testing.T) (server, client net.Conn) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-pc")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, "s"))
	require.NoError(t, err)
	defer l.Close()
	client, err = net.Dial("unix", filepath.Join(dir, "s"))
	require.NoError(t, err)
	server, err = l.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { server.Close(); client.Close() })
	return server, client
}

func TestVerifyPeer_SameUserAccepted(t *testing.T) {
	server, _ := unixPair(t)
	require.NoError(t, verifyPeer(server, os.Getuid()))
}

func TestVerifyPeer_OtherUserRejected(t *testing.T) {
	if !peerCredSupported {
		t.Skip("no peer credentials on " + runtime.GOOS + "; verifyPeer is a documented no-op")
	}
	server, _ := unixPair(t)
	err := verifyPeer(server, os.Getuid()+1)
	require.ErrorContains(t, err, "peer uid")
}

func TestVerifyPeer_NonUnixConn(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	require.Error(t, verifyPeer(a, os.Getuid()))
}

// TestServe_RejectsOtherUser is the S-01 regression at the accept loop: a
// connection whose peer is not the daemon's user is closed before it can
// send a single intent (PTY input means running commands as the user).
func TestServe_RejectsOtherUser(t *testing.T) {
	if !peerCredSupported {
		t.Skip("no peer credentials on " + runtime.GOOS)
	}
	srv, sock, _ := newTestServer(t, state.NewStore())
	srv.ownerUID = os.Getuid() + 1 // pretend the daemon belongs to someone else
	serveTest(t, srv)
	conn := dialTest(t, sock)
	_ = protocol.NewWriter(conn).WriteIntent(protocol.IntentHello, "h", protocol.Hello{})
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, err := protocol.NewReader(conn).Read()
	require.Error(t, err, "the daemon must not answer a foreign peer")
	require.Zero(t, srv.liveConns())
}

func TestServe_SocketIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits do not apply to Windows sockets")
	}
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	fi, err := os.Lstat(sock)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}
