package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The hook dir is the session's own dir, which later holds its transcript:
// spawning a hook-detected tool must create it owner-only.
func TestNewSession_HookDirIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "rex.sock")
	reg := &registry.Registry{Tools: []registry.Tool{{
		ID:      "hooked",
		Command: []string{"sh", "-c"},
		Detect:  registry.Detect{Kind: "hooks", Format: "claude"},
		Models:  []registry.Model{{ID: "m", Args: []string{"exit 0"}}},
	}}}
	srv, err := New(Config{Socket: sock, StateDir: dir, Registry: reg, Store: state.NewStore()})
	require.NoError(t, err)
	serveUntilCleanup(t, srv)

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "hooked", ModelID: "m", Slug: "hooked", CWD: dir,
	}))
	added := readUntil(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))

	for _, d := range []string{filepath.Join(dir, "sessions"), filepath.Dir(state.HookFile(dir, sum.ID))} {
		fi, err := os.Stat(d)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), fi.Mode().Perm(), d)
	}
}
