package setup

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func noHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("GOPATH", "")
}

// Without a home directory doctor reports the rex dirs, the settings file
// and the socket as failed checks rather than testing paths relative to
// the working directory.
func TestDoctorChecks_NoHome(t *testing.T) {
	noHome(t)
	dirs := checkRexDirs()
	require.Len(t, dirs, 1)
	require.Equal(t, checkFail, dirs[0].Status)
	require.Equal(t, checkFail, checkSettingsLoad().Status)
	require.Equal(t, checkFail, checkDaemonReachable(&core.SocketFlag{}).Status)
}

func TestDoctorChecks_UnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, r := range checkRexDirs() {
		require.Equal(t, checkPass, r.Status, r.Name)
	}
	require.Equal(t, checkPass, checkSettingsLoad().Status)
}

// The commands that act on the default daemon refuse to start without a
// home directory; nothing is stopped, removed or installed.
func TestDefaults_NoHome(t *testing.T) {
	noHome(t)
	requireExitCode := func(err error, code int) {
		t.Helper()
		var ee core.ExitError
		require.True(t, errors.As(err, &ee), "%v", err)
		require.Equal(t, code, ee.Code)
	}
	_, err := defaultDaemonControl()
	requireExitCode(err, core.ExitGeneric)
	_, err = defaultUpdateDeps()
	requireExitCode(err, core.ExitGeneric)
	_, err = defaultUninstallDeps()
	requireExitCode(err, core.ExitGeneric)
	_, _, err = detectInstallMode()
	require.Error(t, err, "no GOPATH and no home: ~/go/bin cannot be located")

	requireExitCode(RunSetup([]string{"-preview"}), core.ExitGeneric)
	_, _, err = loadStore()
	require.Error(t, err)
	requireExitCode(configEdit(nil), core.ExitGeneric)
}
