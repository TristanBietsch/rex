package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

func rowsByName(rows []checkResult) map[string]checkResult {
	m := map[string]checkResult{}
	for _, r := range rows {
		m[r.Name] = r
	}
	return m
}

func TestCheckSnapshots_OffCreatesNothing(t *testing.T) {
	home, _ := setupHome(t)
	rows := checkSnapshots()
	require.Equal(t, checkInfo, rows[0].Status)
	require.Contains(t, rows[0].Detail, "off")
	_, err := os.Stat(filepath.Join(home, ".local"))
	require.ErrorIs(t, err, os.ErrNotExist, "doctor must not create the store")
}

func TestCheckSnapshots_OnShowsSizeAndEnvWarning(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home, cfg := setupHome(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("turn_snapshots: true\n"), 0o600))

	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("K=1\n"), 0o600))
	root, err := paths.SnapshotsDir()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(root, home))
	_, err = snapshot.NewStore(root).Capture(context.Background(), repo, "doc00001", snapshot.PhaseStart)
	require.NoError(t, err)

	rows := rowsByName(checkSnapshots())
	require.Equal(t, checkPass, rows["turn snapshots"].Status)
	require.Equal(t, checkInfo, rows["git binary"].Status)
	store := rows["snapshot store: "+repo]
	require.Equal(t, checkInfo, store.Status)
	require.Contains(t, store.Detail, "1 session(s)")
	require.Regexp(t, `\d+(\.\d)? (B|KiB|MiB)`, store.Detail)
	secrets := rows["snapshot secrets: "+repo]
	require.Equal(t, checkWarn, secrets.Status)
	require.Contains(t, secrets.Detail, ".env")
	require.Contains(t, secrets.Detail, ".gitignore")
}

func TestCheckSnapshots_NoGitWarnsWhenOn(t *testing.T) {
	_, cfg := setupHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("turn_snapshots: true\n"), 0o600))
	t.Setenv("PATH", t.TempDir())
	rows := rowsByName(checkSnapshots())
	require.Equal(t, checkWarn, rows["turn snapshots"].Status)
	require.Equal(t, checkWarn, rows["git binary"].Status)
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"} {
		require.Equal(t, want, humanBytes(n))
	}
}
