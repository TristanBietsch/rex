package server

import (
	"bytes"
	"context"
	"encoding/json"
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

const modesTools = `tools:
  - id: echo
    models:
      - id: modes
        name: "Enables alt screen + SGR mouse"
        args: ["printf '\\033[?1049h\\033[?1000h\\033[?1006h'; sleep 5"]
`

func TestSubscribe_ModeSyncSendsPreamble(t *testing.T) {
	dir, err := os.MkdirTemp("", "rex-mode")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	toolsPath := filepath.Join(dir, "tools.yaml")
	require.NoError(t, os.WriteFile(toolsPath, []byte(modesTools), 0o644))
	reg, err := registry.Load(toolsPath)
	require.NoError(t, err)
	sock := filepath.Join(dir, "rex.sock")
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	env := &retireEnv{srv: srv, store: srv.cfg.Store, sock: sock, dir: dir}
	conn, w, r := env.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "modes", Slug: "m", CWD: dir,
	}))
	added := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	// Wait until the child's escapes have reached the supervisor's screen.
	want := []byte("\x1b[?1049h\x1b[?1000h\x1b[?1006h")
	require.Eventually(t, func() bool { return bytes.Equal(srv.ModePreamble(sum.ID), want) }, 3*time.Second, 20*time.Millisecond)

	conn2, w2, r2 := env.dial(t)
	require.NoError(t, w2.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: sum.ID, ModeSync: true}))
	out := waitEvent(t, conn2, r2, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionOutput })
	var so protocol.SessionOutput
	require.NoError(t, json.Unmarshal(out.Data, &so))
	require.Equal(t, want, so.Bytes)
}
