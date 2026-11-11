package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
)

// With turn_snapshots off the daemon does not touch an existing store at
// all: no retention pass, so no gc stamp and no ledger rewrite.
func TestRun_FlagOffLeavesTheStoreAlone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	cfgPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o700))
	require.NoError(t, os.WriteFile(cfgPath, []byte("turn_snapshots: false\nsummary_enabled: false\n"), 0o600))

	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x\n"), 0o600))

	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store := snapshot.NewStore(filepath.Join(dir, "snapshots"))
	_, err = store.Capture(context.Background(), repo, "old00001", snapshot.PhaseStart)
	require.NoError(t, err)
	us, err := store.Usage()
	require.NoError(t, err)
	stamp := filepath.Join(dir, "snapshots", us[0].Key, "last-gc")
	_, err = os.Stat(stamp)
	require.ErrorIs(t, err, os.ErrNotExist)

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
	time.Sleep(500 * time.Millisecond) // a retention pass, if there were one, is long done
	require.NoError(t, c.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop")
	}
	_, err = os.Stat(stamp)
	require.ErrorIs(t, err, os.ErrNotExist, "retention ran with the feature off")
}
