package usage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A ledger path that is a symlink (or anything but a regular file) is never
// read, written or replaced.
func TestLedger_RefusesANonRegularFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "share")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	target := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	l := OpenLedger(filepath.Join(dir, "usage.jsonl"))
	if err := os.Symlink(target, l.Path()); err != nil {
		t.Skip("symlinks unavailable")
	}

	require.ErrorIs(t, l.Append(rec("a", 1, t0)), ErrNotRegular)
	_, err := l.Latest()
	require.ErrorIs(t, err, ErrNotRegular)
	require.ErrorIs(t, l.Trim(t0), ErrNotRegular)
	b, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep\n", string(b), "the link's target was not touched")
	fi, err := os.Lstat(l.Path())
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "the link was not replaced")

	d := OpenLedger(filepath.Join(dir, "adir"))
	require.NoError(t, os.Mkdir(d.Path(), 0o700))
	require.ErrorIs(t, d.Append(rec("a", 1, t0)), ErrNotRegular)
}
