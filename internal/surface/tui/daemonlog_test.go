package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// fakeStart replaces startDaemon for one test and records the log file
// each spawn would have written the daemon's stderr to. No daemon runs.
func fakeStart(t *testing.T) *[]string {
	t.Helper()
	var logs []string
	prev := startDaemon
	startDaemon = func(socket string, logf *os.File) (*daemonctl.StartResult, error) {
		require.NotNil(t, logf, "the daemon must get a stderr log")
		logs = append(logs, logf.Name())
		return &daemonctl.StartResult{PID: 42}, nil
	}
	t.Cleanup(func() { startDaemon = prev })
	return &logs
}

// daemonLogPath is paths.DaemonLog under the test's HOME.
func daemonLogPath(t *testing.T) string {
	t.Helper()
	path, err := paths.DaemonLog()
	require.NoError(t, err)
	return path
}

// requirePrivateLog checks daemon.log and its directory are owner-only.
func requirePrivateLog(t *testing.T, path string) {
	t.Helper()
	require.Equal(t, daemonLogPath(t), path)
	if runtime.GOOS == "windows" {
		return
	}
	for p, want := range map[string]os.FileMode{filepath.Dir(path): 0o700, path: 0o600} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		require.Equal(t, want, info.Mode().Perm(), p)
	}
}

// FU-12 (S-01): the boot splash and the setup wizard opened daemon.log
// 0644 and ignored errors. Both now go through daemonctl.OpenLog, which
// also tightens a log an older version left world-readable.
func TestDaemonSpawns_usePrivateLog(t *testing.T) {
	logPath := daemonLogPath(t)
	testperm.MkdirAll(t, filepath.Dir(logPath), 0o755)
	testperm.WriteFile(t, logPath, []byte("old\n"), 0o644)
	logs := fakeStart(t)
	sock := filepath.Join(t.TempDir(), "rex.sock")

	msg := runBootStep(t, stepDaemon(Model{Socket: sock}))
	require.Equal(t, stepOK, msg.Status, msg.Desc)
	requirePrivateLog(t, (*logs)[0])

	testperm.Loosen(t, logPath, 0o644)
	start, ok := startDaemonCmd(sock)().(setupDaemonStartMsg)
	require.True(t, ok)
	require.NoError(t, start.err)
	requirePrivateLog(t, (*logs)[1])
}

// A log that cannot be opened is reported instead of silently starting a
// daemon whose crash output goes nowhere.
func TestDaemonSpawns_reportLogError(t *testing.T) {
	logs := fakeStart(t)
	// A file where the log directory belongs.
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".local"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".local", "state"), nil, 0o600))
	sock := filepath.Join(t.TempDir(), "rex.sock")

	msg := runBootStep(t, stepDaemon(Model{Socket: sock}))
	require.Equal(t, stepFail, msg.Status)
	require.Contains(t, msg.Desc, "daemon log")

	start, ok := startDaemonCmd(sock)().(setupDaemonStartMsg)
	require.True(t, ok)
	require.ErrorContains(t, start.err, "daemon log")
	require.Empty(t, *logs, "no daemon may start without its log")
}

// runBootStep runs a boot step's command and returns its result message.
func runBootStep(t *testing.T, cmd tea.Cmd) bootStepMsg {
	t.Helper()
	msg, ok := cmd().(bootStepMsg)
	require.True(t, ok)
	return msg
}
