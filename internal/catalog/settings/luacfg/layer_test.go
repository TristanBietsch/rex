package luacfg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// homeWithConfig points HOME at a temp dir holding config.lua and, when yaml
// is not empty, config.yaml.
func homeWithConfig(t *testing.T, lua, yaml string) {
	t.Helper()
	ResetMemo()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lua"), 0o700))
	if lua != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.lua"), []byte(lua), 0o600))
	}
	if yaml != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
	}
	settings.SetLayer(Layer())
	t.Cleanup(func() { settings.SetLayer(nil); ResetMemo() })
}

func TestLayer_EndToEndThroughLoadDefault(t *testing.T) {
	homeWithConfig(t, "rex.set('theme', 'noir')\nrex.theme('mine', {})", "spinner: moon\ntheme: paper\n")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Equal(t, "noir", s.Get("theme"))
	require.Equal(t, "config.lua:1", s.SetAt("theme"))
	require.Equal(t, "moon", s.Get("spinner"))
	require.Contains(t, rep.Themes, "mine")
	require.True(t, rep.Exists)
}

func TestLayer_FlagOffNeverTouchesTheFiles(t *testing.T) {
	homeWithConfig(t, "rex.set('theme', 'noir')", "lua_config: false\n")
	before := Touches()
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Equal(t, before, Touches(), "no stat, read or eval of config.lua or lua/")
	require.False(t, rep.Exists)
	require.Equal(t, "default", s.Get("theme"))
	require.False(t, s.Locked("theme"))
}

func TestLayer_BrokenFileFailsTrustClosed(t *testing.T) {
	homeWithConfig(t, "rex.set(", "auto_trust_dirs: ~/dev\n")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Error(t, rep.Err)
	require.Equal(t, "", s.Get("auto_trust_dirs"))
}

func TestLayer_NoHomeIsEmpty(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err := OptsForHome()
	require.Error(t, err)
	rep := Layer()()
	require.False(t, rep.Exists)
	require.NoError(t, rep.Err)
}

func TestLayer_CannotSetItsOwnFlag(t *testing.T) {
	homeWithConfig(t, "rex.set('lua_config', false)\nrex.set('lua_config_path', '/x')", "")
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Len(t, rep.Warnings, 2)
	require.True(t, settings.LuaConfigOn(s))
	require.False(t, s.Locked("lua_config"))
}
