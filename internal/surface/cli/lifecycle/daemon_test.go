package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
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
	testperm.WriteFile(t, path, []byte("old\n"), 0o644)

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

// fakeCtl is a daemonCtl over a fake daemon: nothing real is dialed,
// looked up or signaled.
type fakeCtl struct {
	up          bool
	pid         string
	pidErr      error
	shutdownErr error
	calls       []string
	out, errOut bytes.Buffer
}

func (f *fakeCtl) ctl(t *testing.T) daemonCtl {
	return daemonCtl{
		socket: "fake.sock",
		reachable: func(socket string) bool {
			require.Equal(t, "fake.sock", socket)
			return f.up
		},
		shutdown: func(_ context.Context, socket string) error {
			require.Equal(t, "fake.sock", socket)
			f.calls = append(f.calls, "shutdown")
			if f.shutdownErr != nil {
				return f.shutdownErr
			}
			f.up = false
			return nil
		},
		findPID: func() (string, error) { return f.pid, f.pidErr },
		starter: daemonStarter{
			socket:    "fake.sock",
			logPath:   filepath.Join(t.TempDir(), "daemon.log"),
			reachable: func(string) bool { return f.up },
			start: func(string, *os.File) (*daemonctl.StartResult, error) {
				f.calls = append(f.calls, "start")
				f.up = true
				return &daemonctl.StartResult{PID: 7}, nil
			},
			out: &f.out,
		},
		out:    &f.out,
		errOut: &f.errOut,
	}
}

func requireExitCode(t *testing.T, err error, want int) {
	t.Helper()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec), "want an exit error, got %v", err)
	require.Equal(t, want, ec.ExitCode())
}

// FU-11: FindPID errors when the user runs two daemons, and stop used to
// refuse on that. The socket decides; the PID is only shown.
func TestDaemonStop_gatesOnSocketNotPID(t *testing.T) {
	f := &fakeCtl{up: true, pidErr: daemonctl.ErrMultipleDaemons}
	require.NoError(t, f.ctl(t).stop())
	require.Equal(t, []string{"shutdown"}, f.calls)
	require.Equal(t, "stopped rex-daemon (pid unknown) · socket=fake.sock\n", f.out.String())
}

func TestDaemonStop_showsPID(t *testing.T) {
	f := &fakeCtl{up: true, pid: "123"}
	require.NoError(t, f.ctl(t).stop())
	require.Equal(t, "stopped rex-daemon (pid 123) · socket=fake.sock\n", f.out.String())
	require.NotContains(t, f.out.String(), "SIGTERM")
}

// A rex-daemon process that does not answer on this socket (another
// socket, another HOME) is not ours to stop.
func TestDaemonStop_notReachable(t *testing.T) {
	f := &fakeCtl{pid: "123"}
	err := f.ctl(t).stop()
	requireExitCode(t, err, core.ExitDaemonUnreachable)
	require.Contains(t, err.Error(), "fake.sock")
	require.Empty(t, f.calls, "nothing answers on the socket, so nothing is stopped")
}

func TestDaemonStop_shutdownFails(t *testing.T) {
	f := &fakeCtl{up: true, shutdownErr: errors.New("still answering after 10s")}
	err := f.ctl(t).stop()
	requireExitCode(t, err, core.ExitGeneric)
	require.Contains(t, err.Error(), "still answering")
}

func TestDaemonStop_goneBeforeShutdown(t *testing.T) {
	f := &fakeCtl{up: true, shutdownErr: daemonctl.ErrNotRunning}
	requireExitCode(t, f.ctl(t).stop(), core.ExitDaemonUnreachable)
}

// Status reports the socket as the truth: a stray rex-daemon process does
// not make it "running", and a PID lookup failure does not make it "not
// running".
func TestDaemonStatus_socketIsTheTruth(t *testing.T) {
	f := &fakeCtl{up: true, pidErr: daemonctl.ErrMultipleDaemons}
	require.NoError(t, f.ctl(t).status())
	require.Equal(t, "running · socket=fake.sock · pid=unknown\n", f.out.String())

	f = &fakeCtl{up: true, pid: "123"}
	require.NoError(t, f.ctl(t).status())
	require.Equal(t, "running · socket=fake.sock · pid=123\n", f.out.String())

	f = &fakeCtl{pid: "123"}
	requireExitCode(t, f.ctl(t).status(), core.ExitDaemonUnreachable)
	require.Equal(t, "not running · socket=fake.sock\n", f.out.String())
}

// Restart starts the new daemon only after shutdown returns, which is once
// the old daemon has removed its socket; no fixed sleep.
func TestDaemonRestart_stopsThenStarts(t *testing.T) {
	f := &fakeCtl{up: true, pid: "123"}
	require.NoError(t, f.ctl(t).restart())
	require.Equal(t, []string{"shutdown", "start"}, f.calls)
	require.Equal(t, "stopped rex-daemon (pid 123) · socket=fake.sock\nrex-daemon started (pid 7)\n", f.out.String())
}

func TestDaemonRestart_notRunningStillStarts(t *testing.T) {
	f := &fakeCtl{}
	require.NoError(t, f.ctl(t).restart())
	require.Equal(t, []string{"start"}, f.calls)
	require.Contains(t, f.errOut.String(), "not running")
}

// A daemon that will not stop must not get a second daemon started beside
// it on the same socket.
func TestDaemonRestart_stopFailureDoesNotStart(t *testing.T) {
	f := &fakeCtl{up: true, shutdownErr: errors.New("still answering after 10s")}
	err := f.ctl(t).restart()
	requireExitCode(t, err, core.ExitGeneric)
	require.Equal(t, []string{"shutdown"}, f.calls)
}

func TestRunDaemon_help(t *testing.T) {
	for _, a := range []string{"-h", "--help", "help"} {
		require.ErrorIs(t, RunDaemon([]string{a}), core.ErrHelpShown, a)
	}
}
