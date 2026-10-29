package daemonctl

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultPaths_UnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	require.Equal(t, filepath.Join(home, ".local", "share", "rex"), DefaultStateDir())
	require.Equal(t, filepath.Join(home, ".config", "rex", "tools.yaml"), DefaultToolsPath())
	require.Equal(t, filepath.Join(home, ".local", "state", "rex", "daemon.log"), LogPath())
}

func TestDefaultSocket_UsesXDGRuntimeDir(t *testing.T) {
	rt := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("XDG_RUNTIME_DIR", rt)
	require.Equal(t, filepath.Join(rt, "rex.sock"), DefaultSocket())
}

// requirePerm checks path's permission bits (Unix only: Windows has none).
func requirePerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, want, info.Mode().Perm(), path)
}

func TestOpenLog_OwnerOnlyAppend(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Dir(LogPath())

	// Fresh: creates the dir and the log owner-only.
	f, err := OpenLog()
	require.NoError(t, err)
	_, err = f.WriteString("old\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	requirePerm(t, dir, 0o700)
	requirePerm(t, LogPath(), 0o600)

	// Left world-readable by an older build: tightened, and appended to.
	require.NoError(t, os.Chmod(dir, 0o755))
	require.NoError(t, os.Chmod(LogPath(), 0o644))
	f, err = OpenLog()
	require.NoError(t, err)
	_, err = f.WriteString("new\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	requirePerm(t, dir, 0o700)
	requirePerm(t, LogPath(), 0o600)

	b, err := os.ReadFile(LogPath())
	require.NoError(t, err)
	require.Equal(t, "old\nnew\n", string(b))
}
