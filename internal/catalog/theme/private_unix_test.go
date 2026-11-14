//go:build !windows

package theme

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadDir_FifoIsSkippedWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe.yaml"), 0o600))
	writeTheme(t, dir, "ok.yaml", "colors: {}")
	done := make(chan *Catalog, 1)
	go func() { done <- LoadDir(dir) }()
	select {
	case c := <-done:
		require.Contains(t, issueFor(c, "pipe.yaml"), "not a regular file")
		require.Equal(t, []string{"ok"}, c.UserNames())
	case <-time.After(10 * time.Second):
		t.Fatal("LoadDir blocked opening a FIFO")
	}
}

func TestLoadDir_FifoBehindALinkIsSkipped(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "themes")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, syscall.Mkfifo(filepath.Join(base, "pipe"), 0o600))
	require.NoError(t, os.Symlink("../pipe", filepath.Join(dir, "linked.yaml")))
	c := LoadDir(dir)
	require.Contains(t, issueFor(c, "linked.yaml"), "not a regular file")
}
