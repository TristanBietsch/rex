//go:build !windows

package rexlog

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, want, info.Mode().Perm(), "mode of %s", path)
}

// S-01: logs carry session names, paths and errors; other local users must
// not read them.
func TestOpenLogFile_DefaultDirIsPrivate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REX_LOG_DIR", "")

	f, err := openLogFile("perm")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	dir := filepath.Join(home, ".local", "state", "rex")
	requireMode(t, dir, 0o700)
	requireMode(t, filepath.Join(dir, "perm.log"), 0o600)
}

// Files and dirs left world-readable by older versions are tightened.
func TestOpenLogFile_TightensExistingDefaultDirAndFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REX_LOG_DIR", "")
	dir := filepath.Join(home, ".local", "state", "rex")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o755))
	log := filepath.Join(dir, "perm.log")
	require.NoError(t, os.WriteFile(log, []byte("old\n"), 0o644))
	require.NoError(t, os.Chmod(log, 0o644))

	f, err := openLogFile("perm")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	requireMode(t, dir, 0o700)
	requireMode(t, log, 0o600)
}

// REX_LOG_DIR may point at a shared directory (even /tmp); rex creates it
// private but never chmods one that already exists. The log file itself is
// always private.
func TestOpenLogFile_CustomDirIsNotChmodded(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "shared")
	require.NoError(t, os.Mkdir(custom, 0o755))
	require.NoError(t, os.Chmod(custom, 0o755))
	t.Setenv("REX_LOG_DIR", custom)

	f, err := openLogFile("perm")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	requireMode(t, custom, 0o755)
	requireMode(t, filepath.Join(custom, "perm.log"), 0o600)

	fresh := filepath.Join(t.TempDir(), "new", "logs")
	t.Setenv("REX_LOG_DIR", fresh)
	f, err = openLogFile("perm")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	requireMode(t, fresh, 0o700)
}

func TestOpenPrivate_TightensAndAppends(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, PrivateDir(dir))
	requireMode(t, dir, 0o700)

	path := filepath.Join(dir, "daemon.log")
	require.NoError(t, os.WriteFile(path, []byte("one\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))
	f, err := OpenPrivate(path)
	require.NoError(t, err)
	_, err = f.WriteString("two\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	requireMode(t, path, 0o600)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "one\ntwo\n", string(b))
}
