//go:build !windows

package daemonctl

import (
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeIntentDaemon serves DefaultSocket (pointed at a short temp dir) and
// hands each intent to answer, which may reply on w. It returns the intent
// types seen. answer returning true closes the listener (a daemon that shut
// down).
func fakeIntentDaemon(t *testing.T, answer func(typ string, w *protocol.Writer) (stop bool)) func() []string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rxd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	ln, err := net.Listen("unix", DefaultSocket())
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	var seen []string
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			env, err := protocol.NewReader(conn).Read()
			if err != nil { // a Reachable probe
				_ = conn.Close()
				continue
			}
			mu.Lock()
			seen = append(seen, env.Type)
			mu.Unlock()
			stop := answer(env.Type, protocol.NewWriter(conn))
			_ = conn.Close()
			if stop {
				_ = ln.Close()
				return
			}
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// stubSignal records signals instead of sending them, so no test can hit a
// real rex-daemon.
func stubSignal(t *testing.T, result error) *[]syscall.Signal {
	t.Helper()
	var sent []syscall.Signal
	prev := signalDaemon
	signalDaemon = func(sig syscall.Signal) error {
		sent = append(sent, sig)
		return result
	}
	t.Cleanup(func() { signalDaemon = prev })
	return &sent
}

func reject(msg string) func(string, *protocol.Writer) bool {
	return func(_ string, w *protocol.Writer) bool {
		_ = w.WriteEvent(protocol.EventError, "", protocol.ErrorEvent{Code: protocol.ErrCodeBadIntent, Message: msg})
		return false
	}
}

func TestStop_UsesShutdownIntent(t *testing.T) {
	sent := stubSignal(t, nil)
	seen := fakeIntentDaemon(t, func(string, *protocol.Writer) bool { return true })
	require.NoError(t, Stop())
	require.Equal(t, []string{protocol.IntentShutdown}, seen())
	require.Empty(t, *sent)
}

// A daemon from before the Shutdown intent rejects it; fall back to SIGTERM.
func TestStop_FallsBackToSignal(t *testing.T) {
	sent := stubSignal(t, nil)
	fakeIntentDaemon(t, reject(errIntentNotImplemented))
	require.NoError(t, Stop())
	require.Equal(t, []syscall.Signal{syscall.SIGTERM}, *sent)
}

func TestStop_NotRunning(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	sent := stubSignal(t, ErrNotRunning)
	require.ErrorIs(t, Stop(), ErrNotRunning)
	require.Equal(t, []syscall.Signal{syscall.SIGTERM}, *sent)
}

func TestReload_UsesReloadIntent(t *testing.T) {
	sent := stubSignal(t, nil)
	seen := fakeIntentDaemon(t, func(string, *protocol.Writer) bool { return false })
	require.NoError(t, Reload())
	require.Equal(t, []string{protocol.IntentReload}, seen())
	require.Empty(t, *sent)
}

// A failed reload (bad tools.yaml) is reported, not masked by a signal.
func TestReload_ReportsRejection(t *testing.T) {
	sent := stubSignal(t, nil)
	fakeIntentDaemon(t, reject("reload tools.yaml: yaml: line 3: bad indent"))
	err := Reload()
	require.ErrorContains(t, err, "bad indent")
	require.Empty(t, *sent)
}

func TestReload_OldDaemonFallsBackToSIGHUP(t *testing.T) {
	sent := stubSignal(t, nil)
	fakeIntentDaemon(t, reject(errIntentNotImplemented))
	require.NoError(t, Reload())
	require.Equal(t, []syscall.Signal{syscall.SIGHUP}, *sent)
}

func TestPgrepArgs_ThisUserExactName(t *testing.T) {
	require.Equal(t, []string{"-u", strconv.Itoa(os.Geteuid()), "-x", "rex-daemon"}, pgrepArgs())
}
