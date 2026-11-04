package lifecycle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// S-01: the daemon log was opened 0644 (after umask) with the error dropped.
func TestOpenDaemonLog_privateModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "state", "rex")
	path := filepath.Join(dir, "daemon.log")

	f, err := openDaemonLog(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	info, err = os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// Logs written by older versions were world-readable; reopening tightens them.
func TestOpenDaemonLog_tightensExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "daemon.log")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	f, err := openDaemonLog(path)
	require.NoError(t, err)
	_, err = f.WriteString("new\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "old\nnew\n", string(data), "the log is appended to, not truncated")
}

func TestOpenDaemonLog_reportsError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	_, err := openDaemonLog(filepath.Join(blocker, "rex", "daemon.log"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "daemon log")
}

// fakeStarter is a daemonStarter whose daemon never really starts.
type fakeStarter struct {
	up      bool
	startFn func(socket string, log *os.File) (*daemonctl.StartResult, error)
	out     bytes.Buffer
}

func (f *fakeStarter) starter(logPath string) daemonStarter {
	return daemonStarter{
		socket:    "fake.sock",
		logPath:   logPath,
		reachable: func(string) bool { return f.up },
		start:     f.startFn,
		out:       &f.out,
	}
}

func TestDaemonStart_logErrorIsReported(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	f := &fakeStarter{startFn: func(string, *os.File) (*daemonctl.StartResult, error) {
		t.Fatal("daemon started without its log")
		return nil, nil
	}}

	err := f.starter(filepath.Join(blocker, "daemon.log")).run()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec))
	require.Equal(t, core.ExitGeneric, ec.ExitCode())
	require.Contains(t, err.Error(), "daemon log")
}

func TestDaemonStart_alreadyRunning(t *testing.T) {
	f := &fakeStarter{up: true, startFn: func(string, *os.File) (*daemonctl.StartResult, error) {
		t.Fatal("started a second daemon")
		return nil, nil
	}}

	require.NoError(t, f.starter(filepath.Join(t.TempDir(), "daemon.log")).run())
	require.Equal(t, "rex-daemon already running\n", f.out.String())
}

func TestDaemonStart_passesLogAndClosesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.log")
	var got *os.File
	f := &fakeStarter{startFn: func(socket string, log *os.File) (*daemonctl.StartResult, error) {
		require.Equal(t, "fake.sock", socket)
		got = log
		return &daemonctl.StartResult{PID: 42}, nil
	}}

	require.NoError(t, f.starter(path).run())
	require.Equal(t, "rex-daemon started (pid 42)\n", f.out.String())
	require.NotNil(t, got)
	require.Equal(t, path, got.Name())
	_, err := got.WriteString("x")
	require.ErrorIs(t, err, os.ErrClosed, "the CLI must close its copy of the log once the daemon has it")
}

func TestDaemonStart_spawnFailure(t *testing.T) {
	f := &fakeStarter{startFn: func(string, *os.File) (*daemonctl.StartResult, error) {
		return nil, errors.New("spawn rex-daemon: no such file")
	}}

	err := f.starter(filepath.Join(t.TempDir(), "daemon.log")).run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no such file")
}
