//go:build !windows

package workspace

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadConfig_FifoDoesNotBlock(t *testing.T) {
	skipWindows(t)
	root := mkWS(t, filepath.Join(t.TempDir(), "p"), goodJSON)
	require.NoError(t, syscall.Mkfifo(ConfigPath(root), 0o600))
	done := make(chan error, 1)
	go func() { _, err := ReadConfig(root); done <- err }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrUnsafe)
	case <-time.After(10 * time.Second):
		t.Fatal("ReadConfig blocked on a FIFO")
	}
}

// The .rex/ swapped while config.lua is opened (for a link to another
// folder, or for another real folder) is refused, even though the file
// read through it would pass every check on its own.
func TestReadConfig_RexSwappedDuringOpenIsRefused(t *testing.T) {
	skipWindows(t)
	for _, tc := range []struct {
		name string
		swap func(t *testing.T, rexDir, other string)
	}{
		{"symlink", func(t *testing.T, rexDir, other string) {
			require.NoError(t, os.Rename(rexDir, rexDir+".old"))
			require.NoError(t, os.Symlink(other, rexDir))
		}},
		{"another folder", func(t *testing.T, rexDir, other string) {
			require.NoError(t, os.Rename(rexDir, rexDir+".old"))
			require.NoError(t, os.Rename(other, rexDir))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			root := mkWS(t, filepath.Join(base, "p"), goodJSON)
			require.NoError(t, os.WriteFile(ConfigPath(root), []byte("rex.set('theme', 'noir')\n"), 0o600))
			other := filepath.Join(base, "other")
			require.NoError(t, os.MkdirAll(other, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(other, ConfigFileName), []byte("rex.set('theme', 'paper')\n"), 0o600))

			b, err := ReadConfig(root)
			require.NoError(t, err, "unswapped it reads")
			require.Contains(t, string(b), "noir")

			beforeConfigOpen = func() { tc.swap(t, filepath.Join(root, DirName), other) }
			t.Cleanup(func() { beforeConfigOpen = func() {} })
			_, err = ReadConfig(root)
			require.ErrorIs(t, err, ErrUnsafe)
		})
	}
}
