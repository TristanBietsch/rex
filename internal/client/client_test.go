package client_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/server"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestClient_DialHelloSnapshot(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-cli")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := server.New(server.Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	// Wait for socket to appear.
	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()

	snap, err := c.Hello("test")
	require.NoError(t, err)
	require.Equal(t, "all", snap.Filter)
	require.Empty(t, snap.Sessions)
}
