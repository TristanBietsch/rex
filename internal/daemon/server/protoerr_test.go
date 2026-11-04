package server

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// requireErrorThenClose reads the next event, which must be an Error with
// code, and then requires the daemon to close the connection.
func requireErrorThenClose(t *testing.T, conn net.Conn, code string) protocol.ErrorEvent {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	r := protocol.NewReader(conn)
	env, err := r.Read()
	require.NoError(t, err, "the daemon closed without saying why")
	ev := readError(t, env)
	require.Equal(t, code, ev.Code)
	requireDisconnected(t, conn, 3*time.Second)
	return ev
}

// TestProtocolErrors_ReplyThenClose is the S-05 regression on the daemon
// side: a malformed, oversized, truncated or wrong-version line used to
// close the connection silently (the CLI just printed "EOF").
func TestProtocolErrors_ReplyThenClose(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	srv.maxLineBytes = 1024
	serveTest(t, srv)

	t.Run("malformed", func(t *testing.T) {
		conn := dialTest(t, sock)
		_, err := conn.Write([]byte("not json\n"))
		require.NoError(t, err)
		requireErrorThenClose(t, conn, protocol.ErrCodeBadIntent)
	})
	t.Run("wrong version", func(t *testing.T) {
		conn := dialTest(t, sock)
		_, err := conn.Write([]byte(`{"v":99,"kind":"Intent","type":"Hello","data":{}}` + "\n"))
		require.NoError(t, err)
		ev := requireErrorThenClose(t, conn, protocol.ErrCodeUnsupportedVersion)
		require.Contains(t, ev.Message, "99")
	})
	t.Run("oversized", func(t *testing.T) {
		conn := dialTest(t, sock)
		_, err := conn.Write([]byte(strings.Repeat("x", 4096)))
		require.NoError(t, err)
		requireErrorThenClose(t, conn, protocol.ErrCodeLineTooLong)
	})
	t.Run("truncated", func(t *testing.T) {
		conn := dialTest(t, sock)
		_, err := conn.Write([]byte(`{"v":1,"kind":"Intent"`))
		require.NoError(t, err)
		require.NoError(t, conn.(*net.UnixConn).CloseWrite())
		requireErrorThenClose(t, conn, protocol.ErrCodeBadIntent)
	})
	t.Run("clean EOF stays silent", func(t *testing.T) {
		conn := dialTest(t, sock)
		helloOnConn(t, conn)
		require.NoError(t, conn.(*net.UnixConn).CloseWrite())
		requireDisconnected(t, conn, 3*time.Second)
	})
}
