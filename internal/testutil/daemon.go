// Package testutil provides shared fixtures for rex integration tests.
package testutil

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// Daemon is a running rex-daemon test instance.
type Daemon struct {
	Socket   string
	StateDir string
	cancel   context.CancelFunc
}

// Close stops the daemon and removes temp state when the test used MkdirTemp.
func (d *Daemon) Close() {
	if d.cancel != nil {
		d.cancel()
	}
}

// StartDaemon boots server.Serve on a temp unix socket and waits until dial succeeds.
func StartDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := server.New(server.Config{
		Socket:   sock,
		StateDir: dir,
		Registry: reg,
		Store:    state.NewStore(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("unix", sock, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond)

	return &Daemon{Socket: sock, StateDir: dir, cancel: cancel}
}

// WaitForSocket dials until the unix socket accepts or the timeout elapses.
func WaitForSocket(socket string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
