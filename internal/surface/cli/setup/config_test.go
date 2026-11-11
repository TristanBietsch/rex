package setup

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestConfigSet_AutoTrustRefused(t *testing.T) {
	require.NoError(t, configSet([]string{"spinner", "moon"}))
	_, path, err := loadStore()
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	for _, bad := range []string{"/", "~", "rel", "a/../b"} {
		err := configSet([]string{"auto_trust_dirs", bad})
		var ec core.ExitCoder
		require.ErrorAs(t, err, &ec, bad)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), bad)
	}

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}
