package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Save is sparse, so answering "no, always ask" must delete an auto_trust_dirs
// saved earlier; otherwise rex keeps auto-trusting after the user said no.
func TestSetupWizardWrite_NoDeletesSavedAutoTrust(t *testing.T) {
	m, cfg := trustWizard(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	trusted := t.TempDir()
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: "+trusted+"\nmaster_volume: 20\n"), 0o600))
	require.False(t, m.trustYes)

	require.Equal(t, "", writeAndLoad(t, m, cfg))
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "auto_trust_dirs")
	require.Contains(t, string(raw), "master_volume: 20", "unrelated keys are kept")
}
