package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// retainSnapshots keeps what a live session owns, collects garbage once, and
// logs (never fails) when the store is unreadable.
func TestRetainSnapshots(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x\n"), 0o600))

	root := filepath.Join(t.TempDir(), "snapshots")
	st := snapshot.NewStore(root)
	_, err = st.Capture(context.Background(), repo, "live0001", snapshot.PhaseStart)
	require.NoError(t, err)
	sessions := state.NewStore()
	require.NoError(t, sessions.Add(&state.Session{ID: "live0001", ShortID: "live", Slug: "live", ToolID: "echo",
		CWD: repo, State: protocol.StateNeedsInput}))

	retainSnapshots(context.Background(), st, sessions)
	es, err := st.Entries(repo)
	require.NoError(t, err)
	require.Len(t, es, 1, "a live session's captures are kept")
	us, err := st.Usage()
	require.NoError(t, err)
	require.Len(t, us, 1)

	// An unreadable root is logged, not fatal.
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	retainSnapshots(context.Background(), snapshot.NewStore(file), sessions)
}
