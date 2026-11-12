package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type fakeWS struct {
	mu      sync.Mutex
	tag     string
	started []string
}

func (f *fakeWS) Tag(s *state.Session) { s.WorkspaceID = f.tag }
func (f *fakeWS) Started(s *state.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, s.ID)
}

// spawnAdded sends payload as a NewSession intent and returns the added
// session as announced to clients.
func spawnAdded(t *testing.T, hook WorkspaceHook, payload map[string]any) protocol.SessionSummary {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-ws")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	reg, err := registry.Load("")
	require.NoError(t, err)
	cfg := Config{
		Socket: filepath.Join(dir, "rex.sock"), StateDir: dir, Registry: reg, Store: state.NewStore(),
		RetireSweepInterval: time.Hour,
	}
	if hook != nil {
		cfg.Workspaces = hook
	}
	srv, err := New(cfg)
	require.NoError(t, err)
	serveTest(t, srv)
	conn := dialTest(t, cfg.Socket)
	w, r := helloOnConn(t, conn)
	payload["cwd"] = dir
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n1", payload))
	for {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		if env.Type == protocol.EventSessionAdded {
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			return sum
		}
	}
}

// The daemon alone decides a session's workspace: a workspace_id in the
// NewSession payload is ignored, whether or not a tagger is wired.
func TestSpawn_ClientCannotSetWorkspaceID(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"tool_id": "echo", "model_id": "short", "slug": "ws-a", "workspace_id": "deadbeef"}
	}
	require.Empty(t, spawnAdded(t, nil, base()).WorkspaceID)

	f := &fakeWS{tag: ""}
	require.Empty(t, spawnAdded(t, f, base()).WorkspaceID, "a tagger that finds nothing leaves it empty")
}

func TestSpawn_TagsBeforeAnnounceAndIndexesAfterStart(t *testing.T) {
	f := &fakeWS{tag: "7f3b89a2"}
	sum := spawnAdded(t, f, map[string]any{"tool_id": "echo", "model_id": "short", "slug": "ws-b"})
	require.Equal(t, "7f3b89a2", sum.WorkspaceID)
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.started) == 1 && f.started[0] == sum.ID
	}, 3*time.Second, 10*time.Millisecond)
}
