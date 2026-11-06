package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// noHome unsets every variable a default rex path could come from.
func noHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
}

func requireExit(t *testing.T, err error, code int) {
	t.Helper()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec), "%v is not an ExitCoder", err)
	require.Equal(t, code, ec.ExitCode(), "%v", err)
}

// Without a home directory there is no default socket or daemon log. Every
// daemon subcommand fails before it touches a socket, a process or a file,
// instead of using paths relative to the working directory.
func TestRunDaemon_NoHome(t *testing.T) {
	noHome(t)
	for _, sub := range []string{"start", "stop", "status", "restart", "logs"} {
		err := RunDaemon([]string{sub})
		requireExit(t, err, core.ExitGeneric)
		require.ErrorContains(t, err, "home directory", sub)
	}
	requireExit(t, RunReload(nil), core.ExitGeneric)
}

func TestRunDaemon_Usage(t *testing.T) {
	requireExit(t, RunDaemon(nil), core.ExitInvalidArgs)
	requireExit(t, RunDaemon([]string{"bogus"}), core.ExitInvalidArgs)
	requireExit(t, RunDaemon([]string{"logs", "-bogus"}), core.ExitInvalidArgs)
	requireExit(t, RunReload([]string{"-bogus"}), core.ExitInvalidArgs)
}

// status and logs resolve the default socket and daemon.log from HOME.
// Nothing answers on the isolated socket, so status reports not running.
func TestRunDaemon_StatusAndLogsUseDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_RUNTIME_DIR", "")

	requireExit(t, RunDaemon([]string{"status"}), core.ExitDaemonUnreachable)
	requireExit(t, RunReload(nil), core.ExitDaemonUnreachable)

	requireExit(t, RunDaemon([]string{"logs"}), core.ExitGeneric) // no log yet
	log := paths.ForHome(home).DaemonLog
	testperm.MkdirAll(t, filepath.Dir(log), 0o700)
	require.NoError(t, os.WriteFile(log, []byte("line\n"), 0o600))
	require.NoError(t, RunDaemon([]string{"logs"}))
}
