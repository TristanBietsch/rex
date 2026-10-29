package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// $EDITOR may carry arguments; exec.Command("code -w", …) found no binary.
func TestEditorCommand(t *testing.T) {
	cmd := editorCommand("code -w", "/x/init.lua")
	require.Equal(t, []string{"code", "-w", "/x/init.lua"}, cmd.Args)

	cmd = editorCommand("  nvim  ", "/x/init.lua")
	require.Equal(t, []string{"nvim", "/x/init.lua"}, cmd.Args)

	cmd = editorCommand("", "/x/init.lua")
	require.Len(t, cmd.Args, 2)
	require.Equal(t, "/x/init.lua", cmd.Args[1])
}

// config edit opens the file the daemon loads, not a hardcoded init.lua.
func TestLuaConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	store := settings.NewStore()
	require.Equal(t, filepath.Join(home, ".config", "rex", "init.lua"), luaConfigPath(store))

	cfg := filepath.Join(home, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("lua_config_path: ~/dots/rex.lua\n"), 0o644))
	require.NoError(t, store.Load(cfg))
	require.Equal(t, filepath.Join(home, "dots", "rex.lua"), luaConfigPath(store))

	require.NoError(t, os.WriteFile(cfg, []byte("lua_config_path: /etc/rex.lua\n"), 0o644))
	require.NoError(t, store.Load(cfg))
	require.Equal(t, "/etc/rex.lua", luaConfigPath(store))
}
