package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// mkConfig makes a workspace with .rex/config.lua holding body and returns
// its root.
func mkConfig(t *testing.T, body string) string {
	t.Helper()
	root := mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	require.NoError(t, os.WriteFile(ConfigPath(root), []byte(body), 0o600))
	return root
}

func TestReadConfig_OK(t *testing.T) {
	root := mkConfig(t, "rex.set('theme', 'noir')\n")
	b, err := ReadConfig(root)
	require.NoError(t, err)
	require.Equal(t, "rex.set('theme', 'noir')\n", string(b))
	require.True(t, HasConfig(root))
}

func TestReadConfig_MissingIsNotExist(t *testing.T) {
	root := mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.False(t, HasConfig(root))
	_, err = ReadConfig(filepath.Join(t.TempDir(), "none"))
	require.ErrorIs(t, err, fs.ErrNotExist, "no .rex/ at all")
	_, err = ReadConfig("rel")
	require.Error(t, err)
}

func TestReadConfig_SymlinkRefused(t *testing.T) {
	skipWindows(t)
	root := mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	target := filepath.Join(t.TempDir(), "elsewhere.lua")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
	require.NoError(t, os.Symlink(target, ConfigPath(root)))
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe, "even a link to a safe file of ours")
	require.True(t, HasConfig(root), "a link still counts as present (it is refused, not ignored)")
}

func TestReadConfig_SymlinkedRexDirRefused(t *testing.T) {
	skipWindows(t)
	real := mkConfig(t, "x")
	root := filepath.Join(t.TempDir(), "q")
	require.NoError(t, os.MkdirAll(root, 0o700))
	require.NoError(t, os.Symlink(filepath.Join(real, DirName), filepath.Join(root, DirName)))
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestReadConfig_GroupWritableRefused(t *testing.T) {
	skipWindows(t)
	root := mkConfig(t, "x")
	testperm.Loosen(t, ConfigPath(root), 0o620)
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe)
	require.ErrorContains(t, err, "writable")

	root = mkConfig(t, "x")
	require.NoError(t, os.Chmod(filepath.Join(root, DirName), 0o770)) //nolint:gosec // the point of the test
	_, err = ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe, "a group-writable .rex/")
}

func TestReadConfig_ForeignOwnerRefused(t *testing.T) {
	skipWindows(t)
	root := mkConfig(t, "x")
	prev := ownedByMe
	t.Cleanup(func() { ownedByMe = prev })
	ownedByMe = func(fi fs.FileInfo) bool { return fi.IsDir() } // the dir is ours, the file is not
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe)
	require.ErrorContains(t, err, "another user")
}

func TestReadConfig_HardLinkRefused(t *testing.T) {
	skipWindows(t)
	root := mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	target := filepath.Join(t.TempDir(), "other.lua")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
	require.NoError(t, os.Link(target, ConfigPath(root)))
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestReadConfig_TooBigAndNotRegular(t *testing.T) {
	root := mkConfig(t, strings.Repeat("-", MaxConfigBytes+1))
	_, err := ReadConfig(root)
	require.ErrorIs(t, err, ErrInvalid)

	root = mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	require.NoError(t, os.Mkdir(ConfigPath(root), 0o700))
	_, err = ReadConfig(root)
	require.ErrorIs(t, err, ErrUnsafe)
}
