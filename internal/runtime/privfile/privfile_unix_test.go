//go:build !windows

package privfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheck_ForeignOwner(t *testing.T) {
	fi, err := os.Stat(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, check(fi, os.Geteuid(), ""))
	require.ErrorContains(t, check(fi, os.Geteuid()+1, ""), "owned by another user")
}

func TestRead_FifoDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600))
	done := make(chan error, 1)
	go func() { _, err := Read(dir, "pipe", 10); done <- err }()
	select {
	case err := <-done:
		require.ErrorContains(t, err, "not a regular file")
	case <-time.After(10 * time.Second):
		t.Fatal("Read blocked opening a FIFO")
	}
}

func TestRead_SymlinkToSafeTargetOK_UnsafeRefused(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "d")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	write(t, base, "safe", "ok", 0o600)
	write(t, base, "unsafe", "no", 0o666)
	require.NoError(t, os.Symlink("../safe", filepath.Join(dir, "s")))
	require.NoError(t, os.Symlink("../unsafe", filepath.Join(dir, "u")))
	require.NoError(t, os.Symlink("../gone", filepath.Join(dir, "g")))
	got, err := Read(dir, "s", 10)
	require.NoError(t, err)
	require.Equal(t, "ok", string(got))
	_, err = Read(dir, "u", 10)
	require.ErrorContains(t, err, "writable by group or others")
	_, err = Read(dir, "g", 10)
	require.ErrorContains(t, err, "cannot open file")
}
