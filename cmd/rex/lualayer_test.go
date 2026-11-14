package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/workspace"
)

func luaHome(t *testing.T) {
	t.Helper()
	luacfg.ResetMemo()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.lua"), []byte("rex.set('theme', 'noir')"), 0o600))
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })
}

// rex complete and rex completion never evaluate config.lua, however the
// output flags are spelled.
func TestInstallLuaLayer_CompleteVerbsNeverEvaluate(t *testing.T) {
	for _, args := range [][]string{
		{"complete", "abcd"}, {"completion", "zsh"}, {"-o", "json", "complete", "abcd"}, {"--agent", "completion", "bash"},
	} {
		luaHome(t)
		before := luacfg.Touches()
		installLuaLayer(args)
		s, _, err := settings.LoadDefault()
		require.NoError(t, err)
		require.Equal(t, before, luacfg.Touches(), args)
		require.Equal(t, "default", s.Get("theme"), args)
	}
}

func TestInstallLuaLayer_OtherVerbsEvaluate(t *testing.T) {
	for _, args := range [][]string{{"ls"}, {}, {"-o", "json", "config", "list"}, {"doctor"}} {
		luaHome(t)
		installLuaLayer(args)
		s, _, err := settings.LoadDefault()
		require.NoError(t, err)
		require.Equal(t, "noir", s.Get("theme"), args)
	}
}

// A trusted workspace's .rex/config.lua applies to commands run inside it,
// never to the system-wide board (bare rex, --quiet-boot).
func TestInstallLuaLayer_WorkspaceConfigOnlyOffTheSystemBoard(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ls"}, "paper"},
		{[]string{"-o", "json", "config", "list"}, "paper"},
		{[]string{"here"}, "paper"},
		{[]string{}, "noir"},
		{[]string{"--quiet-boot"}, "noir"},
		{[]string{"-o", "json"}, "noir"},
	} {
		luaHome(t)
		home, err := paths.Home()
		require.NoError(t, err)
		cfgDir, err := paths.ConfigDir()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("workspaces: true\n"), 0o600))
		root := filepath.Join(home, "proj")
		require.NoError(t, os.MkdirAll(root, 0o700))
		w, err := workspace.Init(root, "proj", nil)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(workspace.ConfigPath(root), []byte("rex.set('theme', 'paper')"), 0o600))
		tp, err := paths.WorkspaceTrustFile()
		require.NoError(t, err)
		ts, err := workspace.OpenTrust(tp)
		require.NoError(t, err)
		require.NoError(t, ts.Trust(w, currentHash(t, w.Root)))
		t.Chdir(root)

		installLuaLayer(tc.args)
		s, _, err := settings.LoadDefault()
		require.NoError(t, err)
		require.Equal(t, tc.want, s.Get("theme"), tc.args)
	}
}

// currentHash is the hash of root's .rex/config.lua now ("" when none).
func currentHash(t *testing.T, root string) string {
	t.Helper()
	b, err := workspace.ReadConfig(root)
	if err != nil {
		return ""
	}
	return workspace.ConfigHash(b)
}
