package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestRun_TurnSnapshotsEndToEnd runs the whole daemon with turn_snapshots on:
// a finished echo session in a git folder has a start and an end capture in
// the store under the state dir, and Undo answers with an UndoResult.
func TestRun_TurnSnapshotsEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	cfgPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o700))
	require.NoError(t, os.WriteFile(cfgPath, []byte("turn_snapshots: true\nsummary_enabled: false\n"), 0o600))

	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o600))

	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	errc := make(chan error, 1)
	go func() { errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", ""}) }()

	var c *client.Client
	require.Eventually(t, func() bool {
		var derr error
		c, derr = client.Dial(sock)
		return derr == nil
	}, 10*time.Second, 20*time.Millisecond)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Hello("test")
	require.NoError(t, err)

	env, err := c.Do(context.Background(), protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "snap-me", CWD: repo,
	})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))

	store := snapshot.NewStore(filepath.Join(dir, "snapshots"))
	require.Eventually(t, func() bool {
		es, err := store.Entries(repo)
		if err != nil {
			return false
		}
		var end bool
		for _, e := range es {
			end = end || e.Phase == snapshot.PhaseEnd
		}
		return end
	}, 30*time.Second, 50*time.Millisecond, "no end capture")

	// Wait for the session to settle, then undo its turn.
	require.Eventually(t, func() bool {
		sc, err := client.Dial(sock)
		if err != nil {
			return false
		}
		defer sc.Close()
		snap, err := sc.Hello("poll")
		if err != nil {
			return false
		}
		for _, s := range snap.Sessions {
			if s.ID == ack.SessionID {
				return s.State == protocol.StateDone
			}
		}
		return false
	}, 20*time.Second, 50*time.Millisecond)
	env, err = c.Do(context.Background(), protocol.IntentUndo, protocol.Undo{SessionID: ack.SessionID})
	require.NoError(t, err)
	require.Equal(t, protocol.EventUndoResult, env.Type)
	var res protocol.UndoResult
	require.NoError(t, json.Unmarshal(env.Data, &res))
	require.NotEmpty(t, res.Safety)

	require.NoError(t, c.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after Shutdown")
	}
}
