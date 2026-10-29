package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func startServer(t *testing.T) (string, string, context.CancelFunc) {
	t.Helper()
	return startServerWithStore(t, state.NewStore())
}

// startServerWithStore is startServer over a caller-owned store, so tests can
// seed sessions (no PTY) before connecting.
func startServerWithStore(t *testing.T, store *state.Store) (string, string, context.CancelFunc) {
	t.Helper()
	// Use os.MkdirTemp with a short prefix so the unix socket path stays within
	// macOS's 104-byte sockaddr_un.sun_path limit (103 usable chars + null).
	dir, err := os.MkdirTemp("", "rex-srv")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = srv.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return")
		}
	})

	// Wait for socket.
	require.Eventually(t, func() bool { return socketLive(sock) }, 2*time.Second, 20*time.Millisecond)
	return sock, dir, cancel
}

func TestNewSession_SpawnsAndCompletes(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()

	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "test"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "test", CWD: t.TempDir(),
	}))

	// Drain events until we see SessionAdded then SessionUpdated to Done.
	gotDone := false
	deadline := time.Now().Add(10 * time.Second)
	for !gotDone && time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(time.Until(deadline))) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionUpdated {
			var upd protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				gotDone = true
			}
		}
	}
	require.True(t, gotDone, "session never reached done")
}
