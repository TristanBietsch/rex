//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func benchRegistry(t *testing.B) *registry.Registry {
	t.Helper()
	path := filepath.Join(repoRoot(t), "testdata", "tools-bench.yaml")
	reg, err := registry.Load(path)
	require.NoError(t, err)
	return reg
}

func repoRoot(t *testing.B) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func startBenchServer(t *testing.B) (sock, stateDir string, cancel context.CancelFunc) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-bench")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock = filepath.Join(dir, "rex.sock")
	reg := benchRegistry(t)
	srv, err := server.New(server.Config{
		Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore(),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return sock, dir, cancel
}

func dialHello(t *testing.B, sock string) (net.Conn, *protocol.Reader, *protocol.Writer) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "bench"}))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)
	return conn, r, w
}

// BenchmarkS1_PTYFlood measures end-to-end daemon time for ~1 MiB echo/flood output.
func BenchmarkS1_PTYFlood(b *testing.B) {
	sock, _, cancel := startBenchServer(b)
	defer cancel()
	cwd := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		conn, r, w := dialHello(b, sock)
		b.StartTimer()

		require.NoError(b, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
			ToolID: "echo", ModelID: "flood", Slug: "flood", CWD: cwd,
		}))

		deadline := time.Now().Add(60 * time.Second)
		done := false
		for !done {
			require.True(b, time.Now().Before(deadline))
			conn.SetReadDeadline(deadline) //nolint:errcheck
			env, err := r.Read()
			require.NoError(b, err)
			if env.Type != protocol.EventSessionUpdated {
				continue
			}
			var upd protocol.SessionUpdated
			require.NoError(b, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				done = true
			}
		}
		b.StopTimer()
		_ = conn.Close()
	}
}

// BenchmarkS2_ConcurrentSessions spawns N sessions and measures Snapshot rebuild cost.
func BenchmarkS2_ConcurrentSessions(b *testing.B) {
	const nSessions = 20
	sock, _, cancel := startBenchServer(b)
	defer cancel()
	cwd := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		conn, r, w := dialHello(b, sock)
		b.StartTimer()

		for j := 0; j < nSessions; j++ {
			require.NoError(b, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
				ToolID: "echo", ModelID: "short",
				Slug: fmt.Sprintf("s%02d", j), CWD: cwd,
			}))
		}

		added := 0
		deadline := time.Now().Add(90 * time.Second)
		for added < nSessions {
			require.True(b, time.Now().Before(deadline))
			conn.SetReadDeadline(deadline) //nolint:errcheck
			env, err := r.Read()
			require.NoError(b, err)
			if env.Type == protocol.EventSessionAdded {
				added++
			}
		}

		// Trigger snapshot-style read: new Hello on same connection is not supported;
		// measure receiving SessionUpdated stream drain for one completion wave instead.
		done := 0
		for done < nSessions {
			require.True(b, time.Now().Before(deadline))
			conn.SetReadDeadline(deadline) //nolint:errcheck
			env, err := r.Read()
			require.NoError(b, err)
			if env.Type != protocol.EventSessionUpdated {
				continue
			}
			var upd protocol.SessionUpdated
			require.NoError(b, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				done++
			}
		}
		b.StopTimer()
		_ = conn.Close()
	}
}
