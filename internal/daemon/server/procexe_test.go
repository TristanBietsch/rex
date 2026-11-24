//go:build linux || darwin

package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcExe_Self(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	got, err := procExe(os.Getpid())
	require.NoError(t, err)
	if runtime.GOOS == "linux" {
		want, err := filepath.EvalSymlinks(self)
		require.NoError(t, err)
		require.Equal(t, want, got)
		return
	}
	// macOS reports the path as given to exec: only the name is stable.
	require.Equal(t, filepath.Base(self), filepath.Base(got))
}

func TestProcExe_GoneProcessFails(t *testing.T) {
	cmd := startChild(t, "true")
	require.NoError(t, cmd.Wait())
	_, err := procExe(cmd.Process.Pid)
	require.Error(t, err)
}
