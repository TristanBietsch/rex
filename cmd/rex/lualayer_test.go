package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
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
