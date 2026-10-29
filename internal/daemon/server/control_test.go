package server

import (
	"context"
	"errors"
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

// startControlServer starts a server with Shutdown/Reload hooks and returns
// its socket plus the serve context (canceled by OnShutdown).
func startControlServer(t *testing.T, onReload func() error) (string, context.Context) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-ctl")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := New(Config{
		Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore(),
		OnShutdown: cancel, OnReload: onReload,
	})
	require.NoError(t, err)
	go func() { _ = srv.Serve(ctx) }()
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sock, ctx
}

func TestIntentShutdown_CancelsServe(t *testing.T) {
	sock, ctx := startControlServer(t, nil)
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	w := protocol.NewWriter(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentShutdown, "", struct{}{}))
	conn.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown intent did not cancel the serve context")
	}
}

func TestIntentReload_CallsHookAndReportsError(t *testing.T) {
	calls := make(chan struct{}, 2)
	fail := errors.New("bad tools.yaml")
	n := 0
	sock, _ := startControlServer(t, func() error {
		calls <- struct{}{}
		n++
		if n == 2 {
			return fail
		}
		return nil
	})
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := protocol.NewWriter(conn), protocol.NewReader(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentReload, "r1", struct{}{}))
	<-calls
	require.NoError(t, w.WriteIntent(protocol.IntentReload, "r2", struct{}{}))
	<-calls
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type)
	require.Equal(t, "r2", env.ID)
	// A tools.yaml that fails to load is a registry problem, not a bad intent.
	require.Equal(t, protocol.ErrCodeRegistry, decodeError(t, env).Code)
}

func TestIntentShutdown_RejectedWithoutHook(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := protocol.NewWriter(conn), protocol.NewReader(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentShutdown, "s", struct{}{}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventError, env.Type)
}
