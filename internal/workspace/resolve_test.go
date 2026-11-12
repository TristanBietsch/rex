package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolve_ByIDByPathAndMisses(t *testing.T) {
	home := isolatedHome(t)
	reg := newReg(t, home)
	root := mkDir(t, home, "dev", "demo")
	nested := mkDir(t, root, "a", "b")
	w, err := Init(root, "demo", reg)
	require.NoError(t, err)

	got, err := Resolve(w.ID, nested, home, reg)
	require.NoError(t, err)
	require.Equal(t, root, got.Root)

	got, err = Resolve("", nested, home, reg)
	require.NoError(t, err, "empty ref is the cwd")
	require.Equal(t, w.ID, got.ID)

	got, err = Resolve("b", filepath.Join(root, "a"), home, reg)
	require.NoError(t, err, "a relative path is under cwd")
	require.Equal(t, w.ID, got.ID)

	got, err = Resolve("~/dev/demo", "/", home, reg)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)

	_, err = Resolve("deadbeef", root, home, reg)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = Resolve("deadbeef", root, home, nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = Resolve(mkDir(t, home, "plain"), root, home, reg)
	require.ErrorIs(t, err, ErrNotFound)

	moved := filepath.Join(home, "dev", "moved")
	require.NoError(t, os.Rename(root, moved))
	_, err = Resolve(w.ID, moved, home, reg)
	require.ErrorIs(t, err, ErrMoved)
}
