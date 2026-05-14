package server

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestServer_AcceptsConnection(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)

	srv, err := New(Config{
		Socket:   sock,
		StateDir: dir,
		Registry: reg,
		Store:    state.NewStore(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Wait for the socket to appear.
	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, err)
	require.NotNil(t, conn)
	defer conn.Close()

	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "1", protocol.Hello{ClientVersion: "test"}))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindEvent, env.Kind)
	require.Equal(t, protocol.EventSnapshot, env.Type)
}
