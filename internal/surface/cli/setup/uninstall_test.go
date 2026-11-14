package setup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// uninstallFixture is a fake rex install under a temp HOME, with a fake
// daemon. Nothing in it touches the real home, binaries or daemon.
type uninstallFixture struct {
	home      string
	rexBin    string
	daemonBin string
	state     string // a file under ~/.local/state/rex
	session   string // a file under ~/.local/share/rex/sessions
	config    string // a file under ~/.config/rex
	profile   string // ~/.zshrc with a REX block
	daemon    *fakeDaemon
	stdin     string
	out       bytes.Buffer
	errOut    bytes.Buffer
}

func newUninstallFixture(t *testing.T) *uninstallFixture {
	t.Helper()
	// Resolve the temp dir: on macOS it sits behind the /var -> /private/var
	// symlink, and uninstall compares resolved paths.
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	f := &uninstallFixture{
		home:      home,
		rexBin:    filepath.Join(home, ".local", "bin", "rex"),
		daemonBin: filepath.Join(home, ".local", "bin", "rex-daemon"),
		state:     filepath.Join(home, ".local", "state", "rex", "daemon.log"),
		session:   filepath.Join(home, ".local", "share", "rex", "sessions", "s1", "meta.json"),
		config:    filepath.Join(home, ".config", "rex", "config.yaml"),
		profile:   filepath.Join(home, ".zshrc"),
		daemon:    &fakeDaemon{up: true},
	}
	for _, p := range f.files() {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(f.profile, []byte("a\n# BEGIN REX\nb\n# END REX\n"), 0o600))
	return f
}

// files are the fixture's removable artifacts (the profile is rewritten,
// not removed, so it is checked separately).
func (f *uninstallFixture) files() []string {
	return []string{f.rexBin, f.daemonBin, f.state, f.session, f.config}
}

func (f *uninstallFixture) run(args ...string) error {
	installPrinter(f.stdin != "") // a scripted answer means a terminal
	return runUninstall(args, uninstallDeps{
		out:        &f.out,
		errOut:     &f.errOut,
		stdin:      strings.NewReader(f.stdin),
		home:       func() (string, error) { return f.home, nil },
		executable: func() (string, error) { return f.rexBin, nil },
		lookDaemon: func() string { return f.daemonBin },
		daemon:     f.daemon.control(),
		now:        func() time.Time { return testStamp },
	})
}

func requireExitCode(t *testing.T, err error, want int) {
	t.Helper()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec), "error %v carries no exit code", err)
	require.Equal(t, want, ec.ExitCode(), "exit code for %v", err)
}

func requireExists(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		_, err := os.Stat(p)
		require.NoError(t, err, "%s should still exist", p)
	}
}

func requireGone(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		_, err := os.Stat(p)
		require.ErrorIs(t, err, os.ErrNotExist, "%s should be removed", p)
	}
}

// -wipe-state used to delete state under a live daemon: the daemon was
// only stopped with -binaries, and the stop didn't wait.
func TestRunUninstall_refusesWhileDaemonStillRunning(t *testing.T) {
	f := newUninstallFixture(t)
	f.daemon.stubborn = true

	err := f.run("-yes", "-binaries=false", "-wipe-state")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.Contains(t, err.Error(), "still running")
	require.Contains(t, err.Error(), "-force")
	require.Equal(t, 1, f.daemon.stopCount())
	requireExists(t, f.files()...)
}

func TestRunUninstall_forceProceedsWhenDaemonWontStop(t *testing.T) {
	f := newUninstallFixture(t)
	f.daemon.stubborn = true

	require.NoError(t, f.run("-yes", "-binaries=false", "-wipe-state", "-force"))
	requireGone(t, f.state, f.session)
	requireExists(t, f.rexBin, f.config)
	require.Contains(t, f.errOut.String(), "still running")
}

func TestRunUninstall_stopsDaemonBeforeRemovingAnything(t *testing.T) {
	f := newUninstallFixture(t)
	f.daemon.downAfter = 3
	var presentAtStop []string
	f.daemon.onStop = func() {
		for _, p := range f.files() {
			if _, err := os.Stat(p); err == nil {
				presentAtStop = append(presentAtStop, p)
			}
		}
	}

	require.NoError(t, f.run("-yes", "-all"))
	require.Equal(t, f.files(), presentAtStop, "something was removed before the daemon was stopped")
	require.False(t, f.daemon.up)
	requireGone(t, f.files()...)
	requireFileBytes(t, f.profile, "a\n")
	requireFileBytes(t, f.profile+".rex-backup-1700000000", "a\n# BEGIN REX\nb\n# END REX\n")
	require.Contains(t, f.out.String(), f.profile+".rex-backup-1700000000")
}

func TestRunUninstall_dryRunTouchesNothing(t *testing.T) {
	f := newUninstallFixture(t)

	require.NoError(t, f.run("-dry-run", "-all"))
	require.Zero(t, f.daemon.stopCount())
	requireExists(t, f.files()...)
	requireFileBytes(t, f.profile, "a\n# BEGIN REX\nb\n# END REX\n")
	require.Contains(t, f.out.String(), "dry-run: nothing removed")
	require.Contains(t, f.out.String(), f.profile)
}

// Stripping profile lines doesn't affect the daemon, so it must not kill
// the user's running sessions.
func TestRunUninstall_stripProfileOnlyLeavesDaemonRunning(t *testing.T) {
	f := newUninstallFixture(t)

	require.NoError(t, f.run("-yes", "-binaries=false", "-strip-profile"))
	require.Zero(t, f.daemon.stopCount())
	require.True(t, f.daemon.up)
	requireFileBytes(t, f.profile, "a\n")
}

func TestRunUninstall_declinedConfirmationStopsNothing(t *testing.T) {
	f := newUninstallFixture(t)
	f.stdin = "n\n"

	err := f.run("-binaries=false", "-wipe-state")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.Zero(t, f.daemon.stopCount())
	requireExists(t, f.files()...)
}

func TestRunUninstall_confirmedRemoves(t *testing.T) {
	f := newUninstallFixture(t)
	f.stdin = "y\n"

	require.NoError(t, f.run("-binaries=false", "-purge-config"))
	require.Equal(t, 1, f.daemon.stopCount())
	requireGone(t, f.config)
	requireExists(t, f.rexBin, f.state)
}

func TestRunUninstall_systemBinaryRefusedBeforeStop(t *testing.T) {
	f := newUninstallFixture(t)
	f.rexBin = filepath.Join(t.TempDir(), "usr", "local", "bin", "rex")

	err := f.run("-yes")
	requireExitCode(t, err, core.ExitOperationRefused)
	require.Contains(t, err.Error(), "system location")
	require.Zero(t, f.daemon.stopCount())
}

func TestRunUninstall_badFlag(t *testing.T) {
	f := newUninstallFixture(t)
	requireExitCode(t, f.run("-nope"), core.ExitInvalidArgs)
	require.Zero(t, f.daemon.stopCount())
}

func TestIsUnder(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "al")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(home, ".local", "bin", "rex"), true},
		{home, true},
		{home + "ice", false}, // /home/alice is not inside /home/al
		{filepath.Join(string(filepath.Separator)+"usr", "local", "bin", "rex"), false},
		{filepath.Join(home, "..", "bob", "rex"), false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, isUnder(c.path, home), "isUnder(%q, %q)", c.path, home)
	}
}

// A removal that fails is reported and makes uninstall exit non-zero, so a
// script can tell a partial uninstall from a clean one.
func TestRunUninstall_failureExitsNonZero(t *testing.T) {
	f := newUninstallFixture(t)
	// An unterminated block is left alone and reported as an error.
	require.NoError(t, os.WriteFile(f.profile, []byte("a\n# BEGIN REX\nb\n"), 0o600))

	err := f.run("-yes", "-binaries=false", "-strip-profile")
	requireExitCode(t, err, core.ExitGeneric)
	require.Contains(t, f.errOut.String(), "error stripping profile")
	requireFileBytes(t, f.profile, "a\n# BEGIN REX\nb\n")
}
