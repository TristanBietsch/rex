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
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/registry"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestMaxConcurrentSessions_RejectsOverflow(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-conc")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	sock := filepath.Join(dir, "rex.sock")
	reg, err := registry.Load("")
	require.NoError(t, err)
	srv, err := New(Config{
		Socket:                sock,
		StateDir:              dir,
		Registry:              reg,
		Store:                 state.NewStore(),
		MaxConcurrentSessions: 1,
	})
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

	// First NewSession (long-running): should succeed.
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1", protocol.NewSession{
		ToolID: "echo", ModelID: "long", Slug: "s1", CWD: dir,
	}))
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			break
		}
	}

	// Second NewSession should hit the cap and emit Error with code "too_many_sessions".
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n2", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "s2", CWD: dir,
	}))
	gotError := false
	deadline := time.Now().Add(3 * time.Second)
	for !gotError && time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventError {
			var ee protocol.ErrorEvent
			require.NoError(t, json.Unmarshal(env.Data, &ee))
			if ee.Code == protocol.ErrCodeTooManySessions {
				gotError = true
			}
		}
	}
	require.True(t, gotError, "second NewSession should have errored with too_many_sessions")
}
