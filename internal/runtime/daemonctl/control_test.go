package daemonctl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
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

// FU-13: a reload the daemon rejects (a tools.yaml it cannot load) is
// reported to the caller. Reload used to be fire-and-forget, so the error
// only reached the daemon log.
func TestReloadDaemon_ReportsRejection(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemonWith(t, sock, fakeOpts{reloadErr: "parse user registry: bad yaml"}, nil)

	err := ReloadDaemon(bg(), sock)
	var de *client.Error
	require.ErrorAs(t, err, &de)
	require.Equal(t, protocol.ErrCodeBadIntent, de.Code)
	require.ErrorContains(t, err, "parse user registry: bad yaml")
	require.NotErrorIs(t, err, ErrNotRunning)
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentHello)
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentReload)
}

// A daemon older than acks never answers Reload: it still gets the intent,
// fire-and-forget as before, and ReloadDaemon does not wait for a reply.
func TestReloadDaemon_PreAckDaemon(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemonWith(t, sock, fakeOpts{legacy: true}, nil)

	start := time.Now()
	require.NoError(t, ReloadDaemon(bg(), sock))
	require.Less(t, time.Since(start), time.Second, "waited for an Ack an old daemon never sends")
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentHello)
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentReload)
}

func TestReloadDaemon_SendsReloadIntent(t *testing.T) {
	sock := socketIn(t)
	fd := startFakeDaemon(t, sock, nil)
	require.NoError(t, ReloadDaemon(bg(), sock))
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentHello)
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
	fd.requireIntent(t, string(protocol.KindIntent)+":"+protocol.IntentHello)
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

// R20: an R20 daemon answers Reload with a report; ReloadDaemonReport
// returns it, and ReloadDaemon turns its errors into an error.
func TestReloadDaemonReport_ReturnsTheReport(t *testing.T) {
	sock := socketIn(t)
	want := protocol.ReloadReport{Changed: []string{"theme"}, Restart: []string{"summary_model"}, Reloaded: []string{"tools.yaml"}}
	startFakeDaemonWith(t, sock, fakeOpts{report: &want}, nil)
	got, err := ReloadDaemonReport(bg(), sock)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.NoError(t, ReloadDaemon(bg(), sock))

	sock = socketIn(t)
	bad := protocol.ReloadReport{Errors: []string{"init.lua:2: boom"}}
	startFakeDaemonWith(t, sock, fakeOpts{report: &bad}, nil)
	got, err = ReloadDaemonReport(bg(), sock)
	require.NoError(t, err, "a failed layer is in the report, not an error")
	require.Equal(t, bad, got)
	require.ErrorContains(t, ReloadDaemon(bg(), sock), "init.lua:2: boom")
}

// A daemon before R20 acks Reload: the report says it is a legacy answer.
func TestReloadDaemonReport_AckDaemon(t *testing.T) {
	sock := socketIn(t)
	startFakeDaemon(t, sock, nil)
	got, err := ReloadDaemonReport(bg(), sock)
	require.NoError(t, err)
	require.True(t, got.Legacy)
}
