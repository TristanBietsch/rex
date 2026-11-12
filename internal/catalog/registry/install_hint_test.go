package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallHint(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	claude, _ := reg.Find("claude")
	require.Equal(t, "see https://code.claude.com/docs/en/setup", claude.InstallHint)

	// The hint is text for the user to read; a user tools.yaml may replace it.
	path := filepath.Join(t.TempDir(), "tools.yaml")
	require.NoError(t, os.WriteFile(path, []byte("tools:\n  - id: claude\n    install_hint: \"see our wiki\"\n"), 0o600))
	reg, err = Load(path)
	require.NoError(t, err)
	claude, _ = reg.Find("claude")
	require.Equal(t, "see our wiki", claude.InstallHint)
}

// The three extra agents are visible when installed (D1): enabled by default.
func TestExtraAgentsEnabledByDefault(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	for _, id := range ExtraAgentIDs {
		tool, ok := reg.Find(id)
		require.True(t, ok, id)
		require.NotNil(t, tool.EnabledByDefault, id)
		require.True(t, *tool.EnabledByDefault, id)
	}
}
