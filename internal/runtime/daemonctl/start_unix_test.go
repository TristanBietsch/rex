//go:build !windows

package daemonctl

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// S-01: daemon.log lives 0600 in a 0700 dir; files older versions created
// 0644 are tightened.
func TestOpenLog_PrivateModes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Dir(LogPath())
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Chmod(dir, 0o755))
	require.NoError(t, os.WriteFile(LogPath(), []byte("old\n"), 0o644))
	require.NoError(t, os.Chmod(LogPath(), 0o644))

	f, err := OpenLog()
	require.NoError(t, err)
	require.NoError(t, f.Close())

	for path, want := range map[string]os.FileMode{dir: 0o700, LogPath(): 0o600} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, want, info.Mode().Perm(), path)
	}

	// A file where the log dir belongs fails cleanly.
	t.Setenv("HOME", LogPath())
	_, err = OpenLog()
	require.Error(t, err)
}

// Start puts the daemon in its own session, so Ctrl-C (SIGINT to the
// terminal's foreground group) in the spawning terminal cannot reach it, and
// reaps it when it exits, so a long-lived TUI collects no zombies.
func TestStart_DetachesAndReaps(t *testing.T) {
	sock := socketIn(t)
	res, err := startFake(t, "serve", sock, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ShutdownDaemon(bg(), sock) })

	sid, err := unix.Getsid(res.PID)
	require.NoError(t, err)
	require.Equal(t, res.PID, sid, "daemon is not a session leader")
	pgid, err := unix.Getpgid(res.PID)
	require.NoError(t, err)
	require.NotEqual(t, unix.Getpgrp(), pgid, "daemon shares the spawner's process group")

	require.NoError(t, ShutdownDaemon(bg(), sock))
	// Signal 0 only probes. A zombie still answers it; a reaped pid does not.
	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(res.PID, 0), syscall.ESRCH)
	}, 5*time.Second, 20*time.Millisecond, "daemon pid %d was not reaped", res.PID)
}
