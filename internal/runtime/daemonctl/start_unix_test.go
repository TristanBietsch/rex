//go:build !windows

package daemonctl

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDaemon puts an executable rex-daemon shell script alone on PATH.
func fakeDaemon(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rex-daemon"), []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	t.Setenv("PATH", dir)
}

// shortSocket returns a socket path well under the sun_path limit (t.TempDir
// paths can exceed it on macOS).
func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rxs")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "rex.sock")
}

// A daemon that dies during startup (bind error, bad registry) must be
// reported with its own error, not as a socket timeout.
func TestStart_ReportsEarlyExitCause(t *testing.T) {
	fakeDaemon(t, `echo 'time=x level=ERROR msg="daemon: serve exited with error"' >&2
echo 'rex-daemon: listen /x: bind: address already in use' >&2
exit 1`)
	logf, err := os.OpenFile(filepath.Join(t.TempDir(), "daemon.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer logf.Close()
	// Output from an earlier run must not be quoted.
	_, err = logf.WriteString("rex-daemon: stale error from last time\n")
	require.NoError(t, err)

	start := time.Now()
	res, err := Start(shortSocket(t), logf)
	require.Nil(t, res)
	require.Error(t, err)
	require.Less(t, time.Since(start), StartTimeout, "early exit should not wait for the timeout")
	require.Contains(t, err.Error(), "exited during startup (exit status 1)")
	require.Contains(t, err.Error(), ": listen /x: bind: address already in use")
	require.NotContains(t, err.Error(), "stale")
}

// The daemon must listen on the socket Start polls, not its own default.
func TestStart_PassesSocket(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	fakeDaemon(t, `echo "$@" > '`+argsFile+`'; exit 1`)
	sock := shortSocket(t)
	_, err := Start(sock, nil)
	require.Error(t, err)
	b, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	require.Equal(t, "-socket "+sock+"\n", string(b))
}

func TestStart_EarlyExitWithoutLog(t *testing.T) {
	fakeDaemon(t, "exit 3")
	_, err := Start(shortSocket(t), nil)
	require.EqualError(t, err, "rex-daemon exited during startup (exit status 3)")
}
