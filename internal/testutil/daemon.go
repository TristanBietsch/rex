// Package testutil provides shared fixtures for rex integration tests.
package testutil

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
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
	served   chan struct{}
	once     sync.Once
}

// Close stops the daemon and waits (bounded) for Serve to return. Safe to
// call more than once; the test's cleanup calls it too. The temp state dir
// is removed by the test cleanup.
func (d *Daemon) Close() {
	d.once.Do(func() {
		d.cancel()
		select {
		case <-d.served:
		case <-time.After(5 * time.Second):
		}
	})
}

// StartDaemon boots server.Serve on a temp unix socket and waits until dial succeeds.
func StartDaemon(t *testing.T) *Daemon {
	t.Helper()
	return StartDaemonWithTools(t, "")
}

// StartDaemonWithTools is StartDaemon with toolsYAML overlaid on the builtin
// tool registry (as ~/.config/rex/tools.yaml would be), so a test can
// register a fake agent program. Empty toolsYAML means builtins only.
func StartDaemonWithTools(t *testing.T, toolsYAML string) *Daemon {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	toolsPath := ""
	if toolsYAML != "" {
		toolsPath = filepath.Join(dir, "tools.yaml")
		require.NoError(t, os.WriteFile(toolsPath, []byte(toolsYAML), 0o644))
	}
	reg, err := registry.Load(toolsPath)
	require.NoError(t, err)
	srv, err := server.New(server.Config{
		Socket:   sock,
		StateDir: dir,
		Registry: reg,
		Store:    state.NewStore(),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{Socket: sock, StateDir: dir, cancel: cancel, served: make(chan struct{})}
	go func() { _ = srv.Serve(ctx); close(d.served) }()
	// Registered after the RemoveAll above, so it runs first.
	t.Cleanup(d.Close)

	require.True(t, WaitForSocket(sock, 2*time.Second), "daemon socket did not come up")
	return d
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
