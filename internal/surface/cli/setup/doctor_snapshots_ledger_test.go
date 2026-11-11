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

// A corrupt ledger is reported, not repaired, by doctor; once the daemon's
// write path has set it aside, doctor says history was reset.
func TestCheckSnapshots_LedgerCorruptThenReset(t *testing.T) {
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
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x\n"), 0o600))
	root, err := paths.SnapshotsDir()
	require.NoError(t, err)
	st := snapshot.NewStore(root)
	ctx := context.Background()
	for _, ph := range []snapshot.Phase{snapshot.PhaseStart, snapshot.PhaseEnd} {
		_, err = st.Capture(ctx, repo, "led00001", ph)
		require.NoError(t, err)
	}
	us, err := st.Usage()
	require.NoError(t, err)
	ledger := filepath.Join(root, us[0].Key, "turns.jsonl")
	b, err := os.ReadFile(ledger)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	//nolint:gosec // the test writes its own temp store
	require.NoError(t, os.WriteFile(ledger, []byte(lines[0]+"\ngarbage\n"+lines[1]+"\n"), 0o600))

	rows := rowsByName(checkSnapshots())
	require.Equal(t, checkWarn, rows["snapshot ledger: "+repo].Status)
	require.Contains(t, rows["snapshot ledger: "+repo].Detail, "corrupt")
	_, err = os.Stat(ledger)
	require.NoError(t, err, "doctor must not rename the ledger")

	_, err = st.Capture(ctx, repo, "led00002", snapshot.PhaseStart) // the daemon's write path
	require.NoError(t, err)
	rows = rowsByName(checkSnapshots())
	require.Contains(t, rows["snapshot ledger: "+repo].Detail, "history was reset")
	require.Contains(t, rows["snapshot ledger: "+repo].Detail, "turns.jsonl.corrupt-")
}
