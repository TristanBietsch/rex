package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// newTestServer builds a Server over store in a short temp dir (macOS caps
// unix socket paths at 104 bytes). It does not start serving.
func newTestServer(t *testing.T, store *state.Store) (srv *Server, sock, dir string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-t")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	reg, err := registry.Load("")
	require.NoError(t, err)
	sock = filepath.Join(dir, "rex.sock")
	srv, err = New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: store})
	require.NoError(t, err)
	return srv, sock, dir
}

// serveTest runs srv.Serve until the test ends, waits until the socket
// accepts, and returns the cancel func plus a channel carrying Serve's
// result. Cleanup fails the test if Serve does not return promptly.
func serveTest(t *testing.T, srv *Server) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", srv.cfg.Socket)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 5*time.Second, 10*time.Millisecond)
	return cancel, served
}

// dialTest connects to sock; the connection closes when the test ends.
func dialTest(t *testing.T, sock string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func liveSession(id string) *state.Session {
	return &state.Session{ID: id, ShortID: id, Slug: id, ToolID: "echo",
		State: protocol.StateWorking, StartedAt: time.Now().UTC()}
}

// requireDisconnected drains conn and fails unless the daemon closes it
// (EOF or reset) within d.
func requireDisconnected(t *testing.T, conn net.Conn, d time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(d)))
	_, err := io.Copy(io.Discard, conn)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("daemon did not close the connection within %s", d)
	}
}

// TestStalledClient_DoesNotBlockOutputOrOtherClients is the P0-01
// regression: a client that says Hello, subscribes and then never reads
// must not stall a session's output broadcast, store events, or the other
// clients. The daemon drops the stalled client instead.
func TestStalledClient_DoesNotBlockOutputOrOtherClients(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("live")))
	srv, sock, _ := newTestServer(t, store)
	// Small enough that the burst below overflows the stalled client's
	// queue long before the test starts draining it.
	srv.clientQueueLen = 64
	serveTest(t, srv)

	// The stalled client reads its snapshot, subscribes, then never reads.
	stalled := dialTest(t, sock)
	sw, _ := helloOnConn(t, stalled)
	require.NoError(t, sw.WriteIntent(protocol.IntentSubscribe, "s", protocol.Subscribe{SessionID: "live"}))

	healthy := dialTest(t, sock)
	hw, hr := helloOnConn(t, healthy)
	require.NoError(t, hw.WriteIntent(protocol.IntentSubscribe, "s", protocol.Subscribe{SessionID: "live"}))
	require.Eventually(t, func() bool {
		srv.outputSubsMu.RLock()
		defer srv.outputSubsMu.RUnlock()
		return len(srv.outputSubs["live"]) == 2
	}, 3*time.Second, 10*time.Millisecond)

	// The healthy client acks every output chunk, which paces the producer
	// to its speed; the stalled client never reads.
	seen := make(chan struct{}, 1)
	go func() {
		for {
			_ = healthy.SetReadDeadline(time.Now().Add(10 * time.Second))
			env, err := hr.Read()
			if err != nil {
				close(seen)
				return
			}
			if env.Type == protocol.EventSessionOutput {
				seen <- struct{}{}
			}
		}
	}()

	const chunks = 512 // 2 MiB of output: far more than any socket buffer
	chunk := []byte(strings.Repeat("x", 4096))
	patch := map[string]any{"description": strings.Repeat("d", 1024)}
	produced := make(chan int, 1)
	go func() {
		n := 0
		defer func() { produced <- n }()
		for range chunks {
			srv.broadcastSessionOutput("live", chunk)
			store.BroadcastPatch("live", patch)
			if _, ok := <-seen; !ok {
				return
			}
			n++
		}
	}()
	select {
	case n := <-produced:
		require.Equal(t, chunks, n, "healthy client lost output")
	case <-time.After(15 * time.Second):
		t.Fatal("a stalled client blocked the session's output broadcast")
	}
	requireDisconnected(t, stalled, 10*time.Second)
}

// TestStalledClient_DoesNotBlockStoreEvents: store mutations (which every
// PTY reader, Delete and the reaper go through) must not block on a client
// that stopped reading after Hello.
func TestStalledClient_DoesNotBlockStoreEvents(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(liveSession("live")))
	srv, sock, _ := newTestServer(t, store)
	serveTest(t, srv)

	stalled := dialTest(t, sock)
	helloOnConn(t, stalled) // reads the snapshot, then never reads again

	done := make(chan struct{})
	go func() {
		defer close(done)
		desc := strings.Repeat("d", 4096)
		for range 4096 { // 16 MiB of patches
			store.BroadcastPatch("live", map[string]any{"description": desc})
		}
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a stalled client blocked store broadcasts")
	}
	requireDisconnected(t, stalled, 10*time.Second)

	// The daemon still serves new clients.
	w, r := helloOnConn(t, dialTest(t, sock))
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "r", protocol.Rename{SessionID: "nope", Title: "x"}))
	env, err := r.Read()
	require.NoError(t, err)
	var ev protocol.ErrorEvent
	require.NoError(t, json.Unmarshal(env.Data, &ev))
	require.Equal(t, protocol.ErrCodeUnknownSession, ev.Code)
}
