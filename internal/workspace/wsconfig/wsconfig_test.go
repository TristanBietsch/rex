package wsconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testenv"
	"github.com/tristanbietsch/rex/internal/workspace"
)

func TestMain(m *testing.M) { testenv.Run(m) }

// env is a temp HOME with config.yaml (and an optional user config.lua) and
// a workspace ~/dev/proj with .rex/config.lua.
type env struct {
	t    *testing.T
	home string
	root string
	ws   *workspace.Workspace
}

func newEnv(t *testing.T, yaml, userLua, wsLua string) *env {
	t.Helper()
	luacfg.ResetMemo()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	cfgDir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0o600))
	if userLua != "" {
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.lua"), []byte(userLua), 0o600))
	}
	root := filepath.Join(home, "dev", "proj")
	require.NoError(t, os.MkdirAll(root, 0o700))
	w, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	if wsLua != "" {
		require.NoError(t, os.WriteFile(workspace.ConfigPath(root), []byte(wsLua), 0o600))
	}
	sub := filepath.Join(root, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	Install(sub)
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })
	return &env{t: t, home: home, root: root, ws: w}
}

func (e *env) trust() {
	e.t.Helper()
	s, err := openTrust()
	require.NoError(e.t, err)
	require.NoError(e.t, s.Trust(e.ws))
}

func (e *env) load() (*settings.Store, settings.LayerReport) {
	e.t.Helper()
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(e.t, err)
	return s, rep
}

const wsOn = "workspaces: true\n"

func TestUntrustedWorkspaceConfigIgnoredWithNotice(t *testing.T) {
	e := newEnv(t, wsOn, "", "rex.set('theme', 'noir')\n")
	before := reads.Load()
	s, rep := e.load()
	require.NotEqual(t, "noir", s.Get("theme"), "not applied before rex ws trust")
	require.Equal(t, settings.SourceDefault, s.Source("theme"))
	require.True(t, rep.Exists)
	require.NoError(t, rep.Err)
	require.Len(t, rep.Warnings, 1)
	require.Contains(t, rep.Warnings[0], "not trusted")
	require.Contains(t, rep.Warnings[0], "rex ws trust")
	require.Contains(t, rep.Warnings[0], e.root)
	require.Equal(t, before, reads.Load(), "the file was never read")
	require.Equal(t, StateUntrusted, Current().State)
}

func TestTrustedWorkspaceConfigApplies_Precedence(t *testing.T) {
	e := newEnv(t, wsOn+"theme: paper\nspinner: moon\nrow_density: compact\n",
		"rex.set('theme', 'noir')\nrex.set('spinner', 'pulse')\n",
		"\nrex.set('theme', 'classic')\n")
	e.trust()
	s, rep := e.load()
	require.NoError(t, rep.Err)
	require.Equal(t, "classic", s.Get("theme"), "workspace beats the user's config.lua")
	require.Equal(t, workspace.ConfigPath(e.root)+":2", s.SetAt("theme"))
	require.True(t, settings.WorkspaceSetAt(s.SetAt("theme")))
	require.Equal(t, "pulse", s.Get("spinner"), "user config.lua beats yaml")
	require.Equal(t, "config.lua:2", s.SetAt("spinner"))
	require.False(t, settings.WorkspaceSetAt(s.SetAt("spinner")))
	require.Equal(t, "compact", s.Get("row_density"), "yaml beats default")
	info := Current()
	require.Equal(t, StateApplied, info.State)
	require.Equal(t, e.ws.ID, info.ID)
	require.Equal(t, e.root, info.Root)
	require.Equal(t, 1, len(info.Report.Values))
}

// Review focus: a cloned repo ships auto_trust_dirs = {"/"}. Even trusted,
// it can never grant or widen trust; config.yaml's values stay.
func TestTrustedWorkspaceCannotGrantTrust(t *testing.T) {
	e := newEnv(t, wsOn+"auto_trust_dirs: ~/dev/a\n", "", `
rex.set("auto_trust_dirs", { "/" })
rex.set("skip_permissions", true)
rex.set("default_spawn_tool", "cursor-agent")
rex.set("extra_agents", true)
rex.set("audit_full_prompt", true)
rex.set("audit_trail", false)
rex.set("workspaces", false)
rex.set("lua_config", false)
rex.set("theme", "noir")
`)
	e.trust()
	s, rep := e.load()
	require.NoError(t, rep.Err)
	require.Equal(t, "~/dev/a", s.String("auto_trust_dirs"))
	require.Equal(t, settings.SourceFile, s.Source("auto_trust_dirs"))
	require.Equal(t, false, s.Get("skip_permissions"))
	require.Equal(t, "claude", s.Get("default_spawn_tool"))
	require.Equal(t, false, s.Get("audit_full_prompt"))
	require.Equal(t, true, s.Get("audit_trail"))
	require.Equal(t, true, s.Get("workspaces"))
	require.Equal(t, "noir", s.Get("theme"), "allowed keys still apply")
	require.Len(t, rep.Warnings, 8)
	require.Empty(t, rep.FailClosed)
}

func TestWorkspacesOffIsInert(t *testing.T) {
	e := newEnv(t, "", "", "rex.set('theme', 'noir')\n")
	e.trust()
	before := looks.Load()
	s, rep := e.load()
	require.NotEqual(t, "noir", s.Get("theme"))
	require.False(t, rep.Exists)
	require.Empty(t, rep.Warnings)
	require.Equal(t, StateOff, Current().State)
	require.Equal(t, before, looks.Load(), "no .rex/ was looked for")
	require.Empty(t, settings.LayerStamp(true), "no stat of .rex/ either (empty user stamp)")
}

func TestWorkspacesSwitchedOnFromUserLua(t *testing.T) {
	e := newEnv(t, "", "rex.set('workspaces', true)\n", "rex.set('theme', 'noir')\n")
	e.trust()
	s, _ := e.load()
	require.Equal(t, "noir", s.Get("theme"), "the effective switch counts")
}

func TestNoFolderIsTheSystemBoard(t *testing.T) {
	e := newEnv(t, wsOn, "", "rex.set('theme', 'noir')\n")
	e.trust()
	Install("")
	s, _ := e.load()
	require.NotEqual(t, "noir", s.Get("theme"))
	require.Equal(t, StateNone, Current().State)
	SetDir(e.root)
	s, _ = e.load()
	require.Equal(t, "noir", s.Get("theme"), "rex ws open sets the folder")
}

// A broken trusted workspace file fails the trust settings closed, as a
// broken user config.lua does; config.yaml's own list is kept for saves.
func TestBrokenWorkspaceConfigFailsClosed(t *testing.T) {
	e := newEnv(t, wsOn+"auto_trust_dirs: ~/dev/a\nskip_permissions: true\n", "", "rex.set('theme',\n")
	e.trust()
	s, rep := e.load()
	require.Error(t, rep.Err)
	require.Contains(t, rep.Err.Error(), workspace.ConfigPath(e.root))
	require.Empty(t, s.String("auto_trust_dirs"))
	require.Equal(t, false, s.Get("skip_permissions"))
	require.Equal(t, "~/dev/a", s.FileString("auto_trust_dirs"))
	require.Equal(t, StateFailed, Current().State)
}

func TestUnsafeTrustedConfigIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no mode bits")
	}
	e := newEnv(t, wsOn+"skip_permissions: true\n", "", "rex.set('theme', 'noir')\n")
	e.trust()
	require.NoError(t, os.Chmod(workspace.ConfigPath(e.root), 0o666))
	s, rep := e.load()
	require.Error(t, rep.Err)
	require.Contains(t, rep.Err.Error(), "writable")
	require.NotEqual(t, "noir", s.Get("theme"))
	require.Equal(t, false, s.Get("skip_permissions"), "fails closed")
}

func TestMovedWorkspaceNeedsTrustAgain(t *testing.T) {
	e := newEnv(t, wsOn, "", "rex.set('theme', 'noir')\n")
	e.trust()
	moved := filepath.Join(e.home, "dev", "moved")
	require.NoError(t, os.Rename(e.root, moved))
	SetDir(moved)
	s, rep := e.load()
	require.NotEqual(t, "noir", s.Get("theme"))
	require.Contains(t, strings.Join(rep.Warnings, "\n"), "not trusted")
}

func TestStampChangesWhenTrustedOrEdited(t *testing.T) {
	e := newEnv(t, wsOn, "", "rex.set('theme', 'noir')\n")
	a := settings.LayerStamp(true)
	e.trust()
	b := settings.LayerStamp(true)
	require.NotEqual(t, a, b, "trusting is seen by a poll")
	require.NoError(t, os.WriteFile(workspace.ConfigPath(e.root), []byte("rex.set('theme', 'paper')  -- longer\n"), 0o600))
	require.NotEqual(t, b, settings.LayerStamp(true))
}
