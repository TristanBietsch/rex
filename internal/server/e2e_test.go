package server

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// TestE2E_FullFlow validates: Hello -> Snapshot -> NewSession -> SessionAdded ->
// SessionUpdated(working) -> SessionUpdated(done) -> meta.json on disk.
func TestE2E_FullFlow(t *testing.T) {
	sock, stateDir, cancel := startServer(t)
	defer cancel()

	conn, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "e2e"}))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)

	cwd := t.TempDir()
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "e2e", CWD: cwd,
	}))

	var sessID string
	gotDone := false
	deadline := time.Now().Add(8 * time.Second)
	for !gotDone {
		require.True(t, time.Now().Before(deadline), "timed out waiting for done")
		conn.SetReadDeadline(deadline) //nolint:errcheck
		env, err := r.Read()
		require.NoError(t, err)
		switch env.Type {
		case protocol.EventSessionAdded:
			var sum protocol.SessionSummary
			require.NoError(t, json.Unmarshal(env.Data, &sum))
			sessID = sum.ID
		case protocol.EventSessionUpdated:
			var upd protocol.SessionUpdated
			require.NoError(t, json.Unmarshal(env.Data, &upd))
			if s, ok := upd.Patch["state"].(string); ok && s == string(protocol.StateDone) {
				gotDone = true
			}
		}
	}
	require.NotEmpty(t, sessID)

	// Verify meta.json on disk (no poll needed — supervisor writes meta before broadcasting done).
	meta := filepath.Join(stateDir, "sessions", sessID, "meta.json")
	_, err = os.Stat(meta)
	require.NoError(t, err, "meta.json must exist on disk after done at %s", meta)
}
