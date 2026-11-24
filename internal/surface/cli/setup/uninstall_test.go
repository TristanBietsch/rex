package setup

import (
	"bytes"
	"encoding/json"
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

// writeAt creates path (and parents) with content.
func writeAt(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// registerWorkspace lists root in the fixture's workspaces.json.
func (f *uninstallFixture) registerWorkspace(t *testing.T, root string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"v": 1, "workspaces": map[string]any{
		"0a1b2c3d": map[string]any{"id": "0a1b2c3d", "name": "w1", "color": "#ff0000", "root": root},
	}})
	require.NoError(t, err)
	writeAt(t, filepath.Join(f.home, ".local", "share", "rex", "workspaces.json"), string(b))
}

func (f *uninstallFixture) dataPaths() []string {
	share := filepath.Join(f.home, ".local", "share", "rex")
	return []string{
		filepath.Join(share, "audit.jsonl"), filepath.Join(share, "usage.jsonl"),
		filepath.Join(share, "snapshots", "x"), filepath.Join(share, "exports", "y"),
		filepath.Join(f.home, ".cache", "rex", "z"),
	}
}

func TestRunUninstall_AllRemovesDataAndCache(t *testing.T) {
	f := newUninstallFixture(t)
	for _, p := range f.dataPaths() {
		writeAt(t, p, "secret")
	}
	require.NoError(t, f.run("-all", "-yes"))
	requireGone(t, f.dataPaths()...)
	requireGone(t, filepath.Join(f.home, ".local", "share", "rex"), filepath.Join(f.home, ".cache", "rex"))
}

func TestRunUninstall_AllRemovesWorkspaceIndexAndAudit_KeepsConfig(t *testing.T) {
	f := newUninstallFixture(t)
	root := filepath.Join(f.home, "proj")
	rex := filepath.Join(root, ".rex")
	for _, n := range []string{"sessions.jsonl", "audit.jsonl", "config.lua", "workspace.json"} {
		writeAt(t, filepath.Join(rex, n), "x")
	}
	f.registerWorkspace(t, root)

	require.NoError(t, f.run("-all", "-yes"))
	requireGone(t, filepath.Join(rex, "sessions.jsonl"), filepath.Join(rex, "audit.jsonl"))
	requireExists(t, filepath.Join(rex, "config.lua"), filepath.Join(rex, "workspace.json"))
	for _, n := range []string{"config.lua", "workspace.json"} {
		require.Contains(t, f.out.String(), filepath.Join(rex, n))
	}
	require.Contains(t, f.out.String(), "kept")
}

func TestRunUninstall_AllSkipsSymlinkedWorkspaceRex(t *testing.T) {
	f := newUninstallFixture(t)
	root := filepath.Join(f.home, "proj")
	target := filepath.Join(f.home, "elsewhere")
	writeAt(t, filepath.Join(target, "sessions.jsonl"), "keep me")
	require.NoError(t, os.MkdirAll(root, 0o700))
	require.NoError(t, os.Symlink(target, filepath.Join(root, ".rex")))
	f.registerWorkspace(t, root)

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, filepath.Join(target, "sessions.jsonl"))
	require.Contains(t, f.out.String(), filepath.Join(root, ".rex"))
	require.Contains(t, f.out.String(), "not followed")
}

func TestRunUninstall_AllSkipsMissingWorkspaceRoot(t *testing.T) {
	f := newUninstallFixture(t)
	gone := filepath.Join(f.home, "gone")
	f.registerWorkspace(t, gone)

	require.NoError(t, f.run("-all", "-yes"))
	require.Contains(t, f.out.String(), gone)
	require.Contains(t, f.out.String(), "no longer exists")
}

func TestRunUninstall_AllListsProfileBackups(t *testing.T) {
	f := newUninstallFixture(t)
	bak := filepath.Join(f.home, ".zshrc.rex-backup-1")
	writeAt(t, bak, "mine")

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, bak)
	require.Contains(t, f.out.String(), bak)
	require.Contains(t, f.out.String(), "kept")
}

func TestRunUninstall_AllRemovesOnlyRexLogsInLogDir(t *testing.T) {
	f := newUninstallFixture(t)
	logs := filepath.Join(f.home, "logs")
	t.Setenv("REX_LOG_DIR", logs)
	writeAt(t, filepath.Join(logs, "daemon.log"), "x")
	writeAt(t, filepath.Join(logs, "other.log"), "x")

	require.NoError(t, f.run("-all", "-yes"))
	requireGone(t, filepath.Join(logs, "daemon.log"))
	requireExists(t, filepath.Join(logs, "other.log"))
}

func TestRunUninstall_AllSkipsLogDirOutsideHome(t *testing.T) {
	f := newUninstallFixture(t)
	logs := t.TempDir()
	t.Setenv("REX_LOG_DIR", logs)
	writeAt(t, filepath.Join(logs, "daemon.log"), "x")

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, filepath.Join(logs, "daemon.log"))
	require.Contains(t, f.out.String(), "outside your home")
}

func TestRunUninstall_AllSkipsSymlinkedDataDir(t *testing.T) {
	f := newUninstallFixture(t)
	other := filepath.Join(f.home, "other")
	writeAt(t, filepath.Join(other, "precious"), "x")
	cache := filepath.Join(f.home, ".cache", "rex")
	require.NoError(t, os.MkdirAll(filepath.Dir(cache), 0o700))
	require.NoError(t, os.Symlink(other, cache))

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, filepath.Join(other, "precious"))
}

func TestRunUninstall_DryRunListsEverything(t *testing.T) {
	f := newUninstallFixture(t)
	for _, p := range f.dataPaths() {
		writeAt(t, p, "secret")
	}
	root := filepath.Join(f.home, "proj")
	writeAt(t, filepath.Join(root, ".rex", "sessions.jsonl"), "x")
	writeAt(t, filepath.Join(root, ".rex", "config.lua"), "x")
	f.registerWorkspace(t, root)
	logs := filepath.Join(f.home, "logs")
	t.Setenv("REX_LOG_DIR", logs)
	writeAt(t, filepath.Join(logs, "tui.log"), "x")
	bak := filepath.Join(f.home, ".zshrc.rex-backup-1")
	writeAt(t, bak, "mine")

	require.NoError(t, f.run("-all", "-dry-run"))
	requireExists(t, f.dataPaths()...)
	requireExists(t, filepath.Join(root, ".rex", "sessions.jsonl"), filepath.Join(logs, "tui.log"))
	out := f.out.String()
	for _, want := range []string{
		filepath.Join(f.home, ".local", "share", "rex"), filepath.Join(f.home, ".cache", "rex"),
		filepath.Join(root, ".rex", "sessions.jsonl"), filepath.Join(root, ".rex", "config.lua"),
		filepath.Join(logs, "tui.log"), bak,
	} {
		require.Contains(t, out, want)
	}
}

type testWarn struct{ t *testing.T }

func (w testWarn) Warn(s string) { w.t.Log("warn:", s) }

// twoDirs returns a resolved temp home and a resolved folder outside it.
func twoDirs(t *testing.T) (home, outside string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	outside, err = filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return home, outside
}

func writeRegistry(t *testing.T, home, root string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"v": 1, "workspaces": map[string]any{
		"0a1b2c3d": map[string]any{"id": "0a1b2c3d", "name": "w", "root": root},
	}})
	require.NoError(t, err)
	writeAt(t, filepath.Join(home, ".local", "share", "rex", "workspaces.json"), string(b))
}

// A .rex swapped for a symlink after the plan was built (the prompt can stay
// open) must not redirect a removal.
func TestUninstallAll_RexSwappedForSymlinkAfterPlan(t *testing.T) {
	home, out := twoDirs(t)
	t.Setenv("REX_LOG_DIR", "")
	root := filepath.Join(home, "proj")
	writeAt(t, filepath.Join(root, ".rex", "sessions.jsonl"), "x")
	victim := filepath.Join(out, "sessions.jsonl")
	writeAt(t, victim, "victim")
	writeRegistry(t, home, root)

	tg := planAll(home)
	require.NotEmpty(t, tg.files)
	require.NoError(t, os.RemoveAll(filepath.Join(root, ".rex")))
	require.NoError(t, os.Symlink(out, filepath.Join(root, ".rex")))
	tg.removeAll(testWarn{t}, nil, 0)
	requireExists(t, victim)
}

func TestUninstallAll_SymlinkedWorkspaceRoot(t *testing.T) {
	home, out := twoDirs(t)
	t.Setenv("REX_LOG_DIR", "")
	victim := filepath.Join(out, ".rex", "sessions.jsonl")
	writeAt(t, victim, "victim")
	root := filepath.Join(home, "proj")
	require.NoError(t, os.Symlink(out, root))
	writeRegistry(t, home, root)

	tg := planAll(home)
	tg.removeAll(testWarn{t}, nil, 0)
	requireExists(t, victim)
	require.Len(t, tg.skipped, 1)
	require.Contains(t, tg.skipped[0].Detail, "symlink")
}

func TestUninstallAll_LogDirSymlinkToShared(t *testing.T) {
	home, out := twoDirs(t)
	victim := filepath.Join(out, "daemon.log")
	writeAt(t, victim, "other program")
	require.NoError(t, os.Symlink(out, filepath.Join(home, "logs")))
	t.Setenv("REX_LOG_DIR", filepath.Join(home, "logs"))

	tg := planAll(home)
	tg.removeAll(testWarn{t}, nil, 0)
	requireExists(t, victim)
	require.NotEmpty(t, tg.skipped)
}

// The log folder swapped for a symlink after the plan.
func TestUninstallAll_LogDirSwappedAfterPlan(t *testing.T) {
	home, out := twoDirs(t)
	logs := filepath.Join(home, "logs")
	writeAt(t, filepath.Join(logs, "daemon.log"), "x")
	victim := filepath.Join(out, "daemon.log")
	writeAt(t, victim, "other program")
	t.Setenv("REX_LOG_DIR", logs)

	tg := planAll(home)
	require.NotEmpty(t, tg.files)
	require.NoError(t, os.RemoveAll(logs))
	require.NoError(t, os.Symlink(out, logs))
	tg.removeAll(testWarn{t}, nil, 0)
	requireExists(t, victim)
}

// A symlinked data or cache folder is skipped and reported, not planned and
// not a failed run.
func TestRunUninstall_AllSymlinkedCacheIsSkippedNotFailed(t *testing.T) {
	f := newUninstallFixture(t)
	other := filepath.Join(f.home, "other")
	writeAt(t, filepath.Join(other, "precious"), "x")
	cache := filepath.Join(f.home, ".cache", "rex")
	require.NoError(t, os.MkdirAll(filepath.Dir(cache), 0o700))
	require.NoError(t, os.Symlink(other, cache))

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, filepath.Join(other, "precious"))
	require.Contains(t, f.out.String(), cache)
	require.Contains(t, f.out.String(), "not followed")
	require.NotContains(t, f.errOut.String(), "error removing")
}

// Every file name rex writes in a log folder. Callers: rexlog.Init in
// cmd/rex-daemon/main.go ("daemon"), setup/setup.go ("setup"),
// setup/onboard.go ("onboard"), meta/tui.go and tui/splash_steps.go
// ("tui"); core/errlog.go writes "cli-errors" and rotates it to ".1".
// Add a name here when a new rexlog caller appears.
var rexLogFilesInUse = []string{
	"daemon.log", "setup.log", "onboard.log", "tui.log", "cli-errors.log", "cli-errors.log.1",
}

func TestRunUninstall_AllRemovesEveryRexLogFile(t *testing.T) {
	f := newUninstallFixture(t)
	logs := filepath.Join(f.home, "logs")
	t.Setenv("REX_LOG_DIR", logs)
	var made []string
	for _, n := range rexLogFilesInUse {
		made = append(made, filepath.Join(logs, n))
		writeAt(t, made[len(made)-1], "x")
	}
	writeAt(t, filepath.Join(logs, "other.log"), "x")

	require.NoError(t, f.run("-all", "-yes"))
	requireGone(t, made...)
	requireExists(t, filepath.Join(logs, "other.log"))
	for _, n := range rexLogFilesInUse {
		require.Contains(t, rexLogFileNames(), n)
	}
}

func TestRunUninstall_AllJSONHasKeptAndSkippedApart(t *testing.T) {
	f := newUninstallFixture(t)
	bak := filepath.Join(f.home, ".zshrc.rex-backup-1")
	writeAt(t, bak, "mine")

	require.NoError(t, f.run("-all", "-dry-run", "-o", "json"))
	var got struct {
		Data struct {
			Plan, Removed, Kept, Skipped []struct{ Path string }
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(f.out.Bytes(), &got), f.out.String())
	var kept []string
	for _, k := range got.Data.Kept {
		kept = append(kept, k.Path)
	}
	require.Contains(t, kept, bak)
	for _, r := range got.Data.Plan {
		require.NotEqual(t, bak, r.Path)
	}
	require.Contains(t, f.out.String(), `"skipped"`)
}

// The backup that this run's -strip-profile writes is kept too: it holds
// the user's own profile, so -all lists it with the older ones.
func TestRunUninstall_AllKeepsBackupWrittenByThisRun(t *testing.T) {
	f := newUninstallFixture(t)
	bak := f.profile + ".rex-backup-1700000000"

	require.NoError(t, f.run("-all", "-yes"))
	requireExists(t, bak)
	out := f.out.String()
	i := strings.Index(out, "# Kept")
	require.GreaterOrEqual(t, i, 0, out)
	require.Contains(t, out[i:], bak)

	g := newUninstallFixture(t)
	require.NoError(t, g.run("-all", "-yes", "-o", "json"))
	var got struct {
		Data struct{ Kept []struct{ Path string } } `json:"data"`
	}
	require.NoError(t, json.Unmarshal(g.out.Bytes(), &got), g.out.String())
	var kept []string
	for _, k := range got.Data.Kept {
		kept = append(kept, k.Path)
	}
	require.Equal(t, []string{g.profile + ".rex-backup-1700000000"}, kept)
}

// TestRunUninstall_ClosingHintNamesAllFolders pins that the closing hint
// names every folder rex writes, not only state and config.
func TestRunUninstall_ClosingHintNamesAllFolders(t *testing.T) {
	f := newUninstallFixture(t)
	require.NoError(t, f.run("-all", "-yes"))
	for _, dir := range []string{"~/.local/state/rex", "~/.local/share/rex", "~/.cache/rex", "~/.config/rex"} {
		require.Contains(t, f.out.String(), dir)
	}
}
