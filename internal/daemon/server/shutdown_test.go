package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
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

// Serve must not return while session supervisors are still killing and
// reaping their agents: main would exit mid-reap.
func TestServe_ShutdownWaitsForSupervisors(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	srv, err := newTestServer(t, sock, dir)
	require.NoError(t, err)
	stop := serveUntilCleanup(t, srv)

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "long", Slug: "long", CWD: dir,
	}))
	added := readUntil(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.Eventually(t, func() bool { return srv.hasStop(sum.ID) }, 2*time.Second, 10*time.Millisecond)

	stop()
	require.False(t, srv.hasStop(sum.ID), "Serve returned before the session's supervisor exited")
	st, ok := srv.cfg.Store.CurrentState(sum.ID)
	require.True(t, ok)
	require.Equal(t, protocol.StateFailed, st, "supervisor records the killed session before Serve returns")
}
