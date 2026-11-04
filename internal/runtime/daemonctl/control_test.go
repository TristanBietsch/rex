package daemonctl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// S-08: Stop goes through the socket's Shutdown intent and waits until the
// daemon has removed its socket, so a restart cannot race the old daemon.
func TestShutdownDaemon_SendsIntentAndWaitsForSocketRemoval(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemon(t, sock, shutdownLikeDaemon(200*time.Millisecond))

	start := time.Now()
	require.NoError(t, ShutdownDaemon(bg(), sock))
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "returned before the socket was removed")
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentShutdown)
	_, err := os.Lstat(sock)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestShutdownDaemon_NotRunning(t *testing.T) {
	sock := socketIn(t)
	require.ErrorIs(t, ShutdownDaemon(bg(), sock), ErrNotRunning)

	// A leftover socket file with nothing listening is not a daemon.
	require.NoError(t, os.WriteFile(sock, nil, 0o600))
	require.ErrorIs(t, ShutdownDaemon(bg(), sock), ErrNotRunning)
}

// A daemon that keeps answering after Shutdown is reported, not waited on
// forever.
func TestShutdownDaemon_TimesOutWhileDaemonAnswers(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemon(t, sock, nil)

	err := shutdownDaemon(bg(), sock, 300*time.Millisecond)
	require.ErrorContains(t, err, "still answering")
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentShutdown)
}

// The daemon stopped answering but its socket file stayed (it crashed mid
// shutdown): reported so a restart knows the old one did not finish.
func TestShutdownDaemon_ReportsSocketLeftBehind(t *testing.T) {
	sock := socketIn(t)
	startFakeDaemon(t, sock, func(fd *fakeDaemon) {
		fd.ln.SetUnlinkOnClose(false)
		_ = fd.ln.Close()
	})

	err := shutdownDaemon(bg(), sock, 300*time.Millisecond)
	require.ErrorContains(t, err, "did not remove")
}

func TestShutdownDaemon_HonorsContext(t *testing.T) {
	sock := socketIn(t)
	startFakeDaemon(t, sock, nil)
	ctx, cancel := context.WithTimeout(bg(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	require.ErrorIs(t, shutdownDaemon(ctx, sock, time.Minute), context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestReloadDaemon_SendsReloadIntent(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemon(t, sock, nil)
	require.NoError(t, ReloadDaemon(bg(), sock))
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentReload)

	require.ErrorIs(t, ReloadDaemon(bg(), socketIn(t)), ErrNotRunning)
}

// S-08: the zero-argument forms act on the default socket only. They used
// to signal whatever `pgrep rex-daemon` matched, across all users.
func TestStopAndReload_UseDefaultSocket(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	require.ErrorIs(t, Reload(), ErrNotRunning)
	require.ErrorIs(t, Stop(), ErrNotRunning)
	StopQuietly()

	sock := filepath.Join(dir, "rex.sock")
	fd := startFakeDaemon(t, sock, shutdownLikeDaemon(0))
	require.NoError(t, Reload())
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentReload)
	require.NoError(t, Stop())
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentShutdown)
	require.False(t, Reachable(sock))
}

// Without a home directory (and no XDG_RUNTIME_DIR) there is no default
// socket: the zero-argument forms fail instead of dialing a CWD-relative
// path.
func TestStopAndReload_NoHome(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.ErrorIs(t, Reload(), paths.ErrNoHome)
	require.ErrorIs(t, Stop(), paths.ErrNoHome)
}
