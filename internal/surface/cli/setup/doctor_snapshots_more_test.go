package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

func TestCheckSnapshots_OldGitWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell script as git")
	}
	_, cfg := setupHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("turn_snapshots: true\n"), 0o600))
	bin := t.TempDir()
	//nolint:gosec // a fake git must be executable
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho 'git version 2.30.1'\n"), 0o700))
	t.Setenv("PATH", bin)
	// A freshly written script can fail to exec once while another test
	// forks (text file busy); a failed version probe is not remembered.
	var rows map[string]checkResult
	require.Eventually(t, func() bool {
		rows = rowsByName(checkSnapshots())
		return strings.Contains(rows["turn snapshots"].Detail, "2.32")
	}, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, checkWarn, rows["turn snapshots"].Status)
	require.Equal(t, checkWarn, rows["git binary"].Status)
}

func TestCheckSnapshots_SizeCapRowAndRepoUntouched(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, cfg := setupHome(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("turn_snapshots: true\n"), 0o600))
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("K=1\n"), 0o600))
	root, err := paths.SnapshotsDir()
	require.NoError(t, err)
	st := snapshot.NewStore(root)
	_, err = st.Capture(context.Background(), repo, "doc00002", snapshot.PhaseStart)
	require.NoError(t, err)
	us, err := st.Usage()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, us[0].Key, "over-cap"), []byte("2026-11-01T00:00:00Z gone0001\n"), 0o600))

	before := dirFingerprint(t, filepath.Join(repo, ".git"))
	rows := rowsByName(checkSnapshots())
	require.Equal(t, checkWarn, rows["snapshot size cap: "+repo].Status)
	require.Contains(t, rows["snapshot size cap: "+repo].Detail, "gone0001")
	require.Equal(t, before, dirFingerprint(t, filepath.Join(repo, ".git")), "doctor must not write to the user's repo")
}

// dirFingerprint is every path and size under dir, plus file contents.
func dirFingerprint(t *testing.T, dir string) string {
	t.Helper()
	var s string
	require.NoError(t, filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		s += p + "\n"
		if !fi.IsDir() {
			b, err := os.ReadFile(p) //nolint:gosec // test reads its own temp dir
			if err != nil {
				return err
			}
			s += string(b) + "\n"
		}
		return nil
	}))
	return s
}
