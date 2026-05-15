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

func TestDelete_RemovesSessionDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-delete")
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

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "doomed", CWD: dir,
	}))

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

	// Wait for the supervisor to actually have created the dir.
	sessDir := filepath.Join(dir, "sessions", sessID)
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(sessDir); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err = os.Stat(sessDir)
	require.NoError(t, err, "session dir should exist before delete")

	require.NoError(t, w.WriteIntent(protocol.IntentDelete, "", protocol.Delete{SessionID: sessID}))

	// Drain events until we see SessionRemoved.
	for i := 0; i < 50; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		env, err := r.Read()
		if err != nil {
			break
		}
		if env.Type == protocol.EventSessionRemoved {
			break
		}
	}

	// Dir should be gone (give the goroutine a tick to settle).
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sessDir); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, statErr := os.Stat(sessDir)
	require.True(t, os.IsNotExist(statErr), "session dir should be removed after delete; got: %v", statErr)
}

func TestSubscribe_ReplaysTranscript(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-replay")
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

	// Pre-seed transcript for a fake session id; the daemon doesn't need to be running it.
	require.NoError(t, state.AppendTranscript(dir, "fake-sess", []byte("PRIOR OUTPUT\n")))

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

	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{
		SessionID: "fake-sess", Replay: true,
	}))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err = r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSessionOutput, env.Type)
	var so protocol.SessionOutput
	require.NoError(t, json.Unmarshal(env.Data, &so))
	require.Equal(t, "PRIOR OUTPUT\n", string(so.Bytes))
}
