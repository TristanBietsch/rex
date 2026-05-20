package daemonctl

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultPaths_UnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.Equal(t, filepath.Join(home, ".local", "share", "rex"), DefaultStateDir())
	require.Equal(t, filepath.Join(home, ".config", "rex", "tools.yaml"), DefaultToolsPath())
	require.Equal(t, filepath.Join(home, ".local", "state", "rex", "daemon.log"), LogPath())
}

func TestDefaultSocket_UsesXDGRuntimeDir(t *testing.T) {
	rt := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("XDG_RUNTIME_DIR", rt)
	require.Equal(t, filepath.Join(rt, "rex.sock"), DefaultSocket())
}
