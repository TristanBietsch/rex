package meta

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// Without a home directory (and no XDG_RUNTIME_DIR) the board has no socket
// to dial: rex fails with a clear error before the TUI starts.
func TestRunTUI_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("REX_LOG_DIR", t.TempDir())

	err := RunTUI(false)
	var ee core.ExitError
	require.True(t, errors.As(err, &ee), "%v", err)
	require.Equal(t, core.ExitGeneric, ee.Code)
	require.Contains(t, ee.Msg, "pass -socket")
}

// A new user (no config, tour never finished) gets the onboard notice; one
// with a config, or who finished the tour, does not.
func TestFirstRunNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	require.Equal(t, onboardNotice, firstRunNotice())

	cfg := filepath.Join(home, ".config", "rex", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("theme: default\n"), 0o600))
	require.Empty(t, firstRunNotice())

	require.NoError(t, os.Remove(cfg))
	marker := filepath.Join(home, ".local", "share", "rex", "onboarded")
	require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
	require.NoError(t, os.WriteFile(marker, []byte("1\n"), 0o600))
	require.Empty(t, firstRunNotice())
}
