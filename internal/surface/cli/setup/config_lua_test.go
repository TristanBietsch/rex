package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// luaConfigHome writes ~/.config/rex/config.lua (and config.yaml when yaml is
// not empty) under the test's HOME and installs the Lua layer for the test.
func luaConfigHome(t *testing.T, lua, yaml string) {
	t.Helper()
	luacfg.ResetMemo()
	dir, err := paths.ConfigDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lua"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.lua"), []byte(lua), 0o600))
	if yaml != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
	}
	settings.SetLayer(luacfg.Layer())
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })
}

func TestConfigSetAndReset_LuaKeyExitsFiveWithLocation(t *testing.T) {
	luaConfigHome(t, "rex.set('spinner', 'moon')\nrex.set('theme', 'noir')", "")
	for name, run := range map[string]func() error{
		"set":   func() error { return configSet([]string{"theme", "paper"}) },
		"reset": func() error { return configReset([]string{"theme"}) },
	} {
		err := run()
		var ec core.ExitCoder
		require.ErrorAs(t, err, &ec, name)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), name)
		require.Contains(t, err.Error(), "config.lua:2", name)
	}
	// config.yaml is untouched; another key still saves.
	require.NoError(t, configSet([]string{"row_density", "compact"}))
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "theme")
	require.NotContains(t, string(raw), "spinner")
}

func TestConfigSet_LuaConfigFlagItselfStaysEditable(t *testing.T) {
	luaConfigHome(t, "rex.set('theme', 'noir')", "")
	require.NoError(t, configSet([]string{"lua_config", "false"}))
	s, _, err := settings.LoadDefault()
	require.NoError(t, err)
	require.Equal(t, "default", s.Get("theme"), "off: config.lua is ignored and rows unlock")
	require.NoError(t, configSet([]string{"theme", "paper"}))
}
