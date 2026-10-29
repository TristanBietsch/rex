package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A socket path over the sun_path limit fails up front with the path and
// the limit, not with a bare "invalid argument" from bind.
func TestRun_SocketPathTooLong(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REX_LOG_DIR", filepath.Join(home, "logs"))
	sock := filepath.Join(home, strings.Repeat("s", 120), "rex.sock")

	err := run([]string{"-socket", sock, "-state-dir", filepath.Join(home, "state")})
	require.Error(t, err)
	require.Contains(t, err.Error(), sock)
	require.Contains(t, err.Error(), "allows at most")
}

// New dirs are owner-only; an existing one is tightened only when it is
// rex's own, never a user-supplied path that may be shared.
func TestOwnerOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	perm := func(dir string) os.FileMode {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		return info.Mode().Perm()
	}

	fresh := filepath.Join(t.TempDir(), "a", "b")
	require.NoError(t, ownerOnlyDir(fresh, false))
	require.Equal(t, os.FileMode(0o700), perm(fresh))

	shared := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(shared, 0o755))
	require.NoError(t, ownerOnlyDir(shared, false))
	require.Equal(t, os.FileMode(0o755), perm(shared))

	owned := filepath.Join(t.TempDir(), "owned")
	require.NoError(t, os.Mkdir(owned, 0o755))
	require.NoError(t, ownerOnlyDir(owned, true))
	require.Equal(t, os.FileMode(0o700), perm(owned))
}
