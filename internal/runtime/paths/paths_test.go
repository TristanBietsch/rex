package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

func TestMain(m *testing.M) {
	testenv.Run(m)
}

// setHome points the home directory at home on every platform
// (os.UserHomeDir reads USERPROFILE on Windows).
func setHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// The on-disk layout is a contract with every existing install: these are
// the locations rex has always used, so no user data moves.
func TestAccessors_UnderHome(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("XDG_RUNTIME_DIR", "")
	j := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }

	cases := []struct {
		name string
		get  func() (string, error)
		want string
	}{
		{"Home", paths.Home, home},
		{"ConfigDir", paths.ConfigDir, j(".config", "rex")},
		{"ConfigFile", paths.ConfigFile, j(".config", "rex", "config.yaml")},
		{"ToolsFile", paths.ToolsFile, j(".config", "rex", "tools.yaml")},
		{"LuaInitFile", paths.LuaInitFile, j(".config", "rex", "init.lua")},
		{"SkillsDir", paths.SkillsDir, j(".config", "rex", "skills")},
		{"ThemesDir", paths.ThemesDir, j(".config", "rex", "themes")},
		{"DataDir", paths.DataDir, j(".local", "share", "rex")},
		{"SessionsDir", paths.SessionsDir, j(".local", "share", "rex", "sessions")},
		{"SnapshotsDir", paths.SnapshotsDir, j(".local", "share", "rex", "snapshots")},
		{"ExportsDir", paths.ExportsDir, j(".local", "share", "rex", "exports")},
		{"DaemonLock", paths.DaemonLock, j(".local", "share", "rex", "daemon.lock")},
		{"UsageLedger", paths.UsageLedger, j(".local", "share", "rex", "usage.jsonl")},
		{"WorkspacesFile", paths.WorkspacesFile, j(".local", "share", "rex", "workspaces.json")},
		{"AuditFile", paths.AuditFile, j(".local", "share", "rex", "audit.jsonl")},
		{"LogDir", paths.LogDir, j(".local", "state", "rex")},
		{"DaemonLog", paths.DaemonLog, j(".local", "state", "rex", "daemon.log")},
		{"TUIStateFile", paths.TUIStateFile, j(".local", "state", "rex", "tui-state.json")},
		{"CacheDir", paths.CacheDir, j(".cache", "rex")},
		{"Socket", paths.Socket, j(".cache", "rex", "rex.sock")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.get()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	l, err := paths.Resolve()
	require.NoError(t, err)
	require.Equal(t, paths.ForHome(home), l)
}

// The daemon lock and session dirs are also named by package state, which
// takes the data dir as its root; both must name the same files.
func TestLayout_AgreesWithState(t *testing.T) {
	l := paths.ForHome(t.TempDir())
	require.Equal(t, filepath.Join(l.DataDir, state.LockFileName), l.DaemonLock)
}

func TestSocket_XDGRuntimeDir(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	rt := filepath.Join(t.TempDir(), "run")

	t.Run("absolute is used", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", rt)
		got, err := paths.Socket()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(rt, "rex.sock"), got)
	})
	t.Run("absolute needs no home", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", rt)
		setHome(t, "")
		got, err := paths.Socket()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(rt, "rex.sock"), got)
	})
	t.Run("relative is ignored", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "run")
		got, err := paths.Socket()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, ".cache", "rex", "rex.sock"), got)
	})
	t.Run("unset falls back to the cache dir", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		got, err := paths.Socket()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, ".cache", "rex", "rex.sock"), got)
	})
}

// A missing or relative home is an error from every accessor: never a path
// relative to the working directory, never a shared fallback such as /tmp.
func TestAccessors_NoHome(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	accessors := map[string]func() (string, error){
		"Home": paths.Home, "ConfigDir": paths.ConfigDir, "ConfigFile": paths.ConfigFile,
		"ToolsFile": paths.ToolsFile, "LuaInitFile": paths.LuaInitFile, "SkillsDir": paths.SkillsDir, "ThemesDir": paths.ThemesDir, "DataDir": paths.DataDir,
		"SessionsDir": paths.SessionsDir, "SnapshotsDir": paths.SnapshotsDir, "DaemonLock": paths.DaemonLock, "ExportsDir": paths.ExportsDir, "UsageLedger": paths.UsageLedger, "LogDir": paths.LogDir,
		"DaemonLog": paths.DaemonLog, "TUIStateFile": paths.TUIStateFile, "CacheDir": paths.CacheDir,
		"Socket": paths.Socket,
	}
	for _, home := range []string{"", "relative/home", "."} {
		t.Run("home="+home, func(t *testing.T) {
			setHome(t, home)
			for name, get := range accessors {
				got, err := get()
				require.ErrorIs(t, err, paths.ErrNoHome, name)
				require.Empty(t, got, name)
			}
			_, err := paths.Resolve()
			require.ErrorIs(t, err, paths.ErrNoHome)
		})
	}
}

func TestIsHomeRelative(t *testing.T) {
	sep := string(filepath.Separator)
	require.True(t, paths.IsHomeRelative("~/dev"))
	require.True(t, paths.IsHomeRelative("~"+sep+"dev"))
	require.True(t, paths.IsHomeRelative("~/"))
	for _, p := range []string{"", "~", "~user/x", "/abs", "rel", "x~/"} {
		require.False(t, paths.IsHomeRelative(p), p)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	got, err := paths.ExpandHome("~/x/init.lua")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "x", "init.lua"), got)

	for _, p := range []string{"", "~", "~user/x", "/abs/init.lua", "rel/init.lua"} {
		got, err := paths.ExpandHome(p)
		require.NoError(t, err)
		require.Equal(t, p, got)
	}

	setHome(t, "")
	_, err = paths.ExpandHome("~/x")
	require.ErrorIs(t, err, paths.ErrNoHome)
	got, err = paths.ExpandHome("/abs")
	require.NoError(t, err, "a path without ~/ never needs the home directory")
	require.Equal(t, "/abs", got)
}
