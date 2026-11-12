package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// CLI output follows a theme config.lua defines, like a theme file.
func TestResolvePalette_LuaTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	settings.SetLayer(func() settings.LayerReport {
		return settings.LayerReport{Exists: true, Themes: map[string]theme.Palette{"lunar": {Name: "lunar", Accent: "#123456"}}}
	})
	t.Cleanup(func() { settings.SetLayer(nil) })
	require.Equal(t, "#123456", resolvePalette("lunar").Accent)
	def, _ := theme.Get(theme.DefaultName)
	require.Equal(t, def.Accent, resolvePalette("nope").Accent)
}
