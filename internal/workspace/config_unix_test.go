//go:build !windows

package workspace

import (
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
