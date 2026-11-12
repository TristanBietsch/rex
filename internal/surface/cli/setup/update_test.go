package setup

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// updateFixture runs runUpdate against a fake daemon and fake upgraders,
// recording the order of side effects in log.
type updateFixture struct {
	mode       installMode
	exe        string
	daemon     *fakeDaemon
	installErr error
	stdin      string
	log        []string
	sourceDir  string
	out        bytes.Buffer
	errOut     bytes.Buffer
}

func newUpdateFixture() *updateFixture {
	f := &updateFixture{
		mode:   installModeGoInstall,
		exe:    filepath.Join("home", "go", "bin", "rex"),
		daemon: &fakeDaemon{up: true},
	}
	f.daemon.log = &f.log
	return f
}

func (f *updateFixture) run(args ...string) error {
	installPrinter(f.stdin != "") // a scripted answer means a terminal
	return runUpdate(args, updateDeps{
		out:    &f.out,
		errOut: &f.errOut,
		stdin:  strings.NewReader(f.stdin),
		detectMode: func() (installMode, string, error) {
			f.log = append(f.log, "detect")
			return f.mode, f.exe, nil
		},
		daemon: f.daemon.control(),
		goInstall: func(bool) error {
			f.log = append(f.log, "go install")
			return f.installErr
		},
		fromSource: func(dir string, _ bool) error {
			f.log = append(f.log, "source")
			f.sourceDir = dir
			return f.installErr
		},
		installedVersion: func() string { return "v9.9.9" },
	})
}

// Every install.sh install is binary-swap, which update can't upgrade. It
// used to stop the daemon first and only then refuse, killing the user's
// sessions for nothing.
func TestRunUpdate_binarySwapRefusesBeforeStoppingDaemon(t *testing.T) {
	f := newUpdateFixture()
	f.mode = installModeBinarySwap

	err := f.run("-yes")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.Equal(t, []string{"detect"}, f.log)
	require.Zero(t, f.daemon.stopCount())
	require.True(t, f.daemon.up)
	require.ErrorContains(t, err, "cannot self-upgrade binary-swap install")
}

func TestRunUpdate_goInstallStopsDaemonThenUpgrades(t *testing.T) {
	f := newUpdateFixture()
	f.daemon.downAfter = 2

	require.NoError(t, f.run("-yes"))
	require.Equal(t, []string{"detect", "stop", "go install"}, f.log)
	require.False(t, f.daemon.up, "upgrade ran before the daemon's socket closed")
	require.Contains(t, f.out.String(), "v9.9.9")
	require.Contains(t, f.out.String(), "daemon will restart on next `rex`")
}

// -source runs the checkout's install script, so it works for binary-swap
// installs too.
func TestRunUpdate_sourceOverridesBinarySwap(t *testing.T) {
	f := newUpdateFixture()
	f.mode = installModeBinarySwap

	require.NoError(t, f.run("-yes", "-source", "/src/rex"))
	require.Equal(t, []string{"detect", "stop", "source"}, f.log)
	require.Equal(t, "/src/rex", f.sourceDir)
}

func TestRunUpdate_daemonNotRunningSkipsStop(t *testing.T) {
	f := newUpdateFixture()
	f.daemon.up = false

	require.NoError(t, f.run("-y"))
	require.Equal(t, []string{"detect", "go install"}, f.log)
}

func TestRunUpdate_declinedStopsNothing(t *testing.T) {
	f := newUpdateFixture()
	f.stdin = "n\n"

	requireExitCode(t, f.run(), core.ExitOperationRefused)
	require.Equal(t, []string{"detect"}, f.log)
}

func TestRunUpdate_acceptsDefaultYes(t *testing.T) {
	f := newUpdateFixture()
	f.stdin = "\n"

	require.NoError(t, f.run())
	require.Equal(t, []string{"detect", "stop", "go install"}, f.log)
}

// A daemon that won't stop is a warning, not a failure: the binary swap is
// still safe and the new daemon starts on the next `rex`.
func TestRunUpdate_daemonWontStopWarns(t *testing.T) {
	f := newUpdateFixture()
	f.daemon.stubborn = true

	require.NoError(t, f.run("-yes"))
	require.Contains(t, f.errOut.String(), "could not stop daemon")
	require.Equal(t, []string{"detect", "stop", "go install"}, f.log)
}

func TestRunUpdate_installFailure(t *testing.T) {
	f := newUpdateFixture()
	f.installErr = errors.New("network down")

	err := f.run("-yes")
	requireExitCode(t, err, core.ExitGeneric)
	require.Contains(t, err.Error(), "network down")
}

func TestRunUpdate_checkTouchesNothing(t *testing.T) {
	f := newUpdateFixture()

	require.NoError(t, f.run("-check"))
	require.Empty(t, f.log)
	require.Contains(t, f.out.String(), core.Version)
}

func TestRunUpdate_badFlag(t *testing.T) {
	f := newUpdateFixture()
	requireExitCode(t, f.run("-nope"), core.ExitInvalidArgs)
	require.Empty(t, f.log)
}

func TestClassifyInstall(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "al")
	gopath := filepath.Join(string(filepath.Separator)+"opt", "go")
	cases := []struct {
		name, exe, gopath string
		want              installMode
	}{
		{"GOPATH bin", filepath.Join(gopath, "bin", "rex"), gopath, installModeGoInstall},
		{"default GOPATH", filepath.Join(home, "go", "bin", "rex"), "", installModeGoInstall},
		{"home go bin with custom GOPATH", filepath.Join(home, "go", "bin", "rex"), gopath, installModeGoInstall},
		{"install.sh", filepath.Join(home, ".local", "bin", "rex"), gopath, installModeBinarySwap},
		{"look-alike dir", filepath.Join(home, "go", "binaries", "rex"), "", installModeBinarySwap},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, classifyInstall(c.exe, home, c.gopath))
		})
	}
}
