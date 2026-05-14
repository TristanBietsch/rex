package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestSubscribe_StreamsSessionOutput(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-stream")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()

	for i := 0; i < 100; i++ {
		if _, err := net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	// Spawn echo/short
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "stream", CWD: dir,
	}))

	// Wait for SessionAdded to learn the ID
	var sessID string
	for sessID == "" {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			sessID = sum.ID
		}
	}

	// Subscribe to this session's output
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sessID}))

	// Read events for ~8s and accumulate output
	var outputMu sync.Mutex
	var output []byte
	deadline := time.Now().Add(8 * time.Second)
	gotHello := false
	for !gotHello && time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionOutput {
			var so protocol.SessionOutput
			require.NoError(t, json.Unmarshal(env.Data, &so))
			outputMu.Lock()
			output = append(output, so.Bytes...)
			outputMu.Unlock()
			if strings.Contains(string(output), "hello from rex") {
				gotHello = true
			}
		}
	}
	require.True(t, gotHello, "expected to see 'hello from rex' streamed via SessionOutput")
}
