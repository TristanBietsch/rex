package server

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Shutdown must not wait for clients to hang up: an idle TUI would otherwise
// keep SIGTERM (and the Shutdown intent) from ever finishing.
func TestServe_ShutdownClosesConnectedClients(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	srv, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	stop := serveUntilCleanup(t, srv)

	idle, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer idle.Close()
	board, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer board.Close()
	helloOnConn(t, board)

	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return while clients were connected")
	}

	// The daemon hung up on them.
	require.NoError(t, board.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err = board.Read(make([]byte, 1))
	require.Error(t, err)
}
