package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// luaModel is reloadModel with the config.lua layer installed. It returns
// config.lua's path.
func luaModel(t *testing.T) (m Model, cfg, themeDir, lua string) {
	t.Helper()
	m, cfg, themeDir = reloadModel(t)
	luacfg.ResetMemo()
	luacfg.Install()
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })
	lua = filepath.Join(filepath.Dir(cfg), "config.lua")
	m.configStamp = configStamp(cfg, themeDir, true)
	return m, cfg, themeDir, lua
}

func TestConfigReloadAppliesConfigLua(t *testing.T) {
	m, cfg, dir, lua := luaModel(t)
	writeFile(t, lua, "rex.set('spinner', 'moon')\nrex.theme('lunar', { extends = 'noir' })\nrex.set('theme', 'lunar')\n")
	m, cmd := runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.NotNil(t, cmd)
	require.Empty(t, m.Err)
	require.Equal(t, "moon", m.Store.Get("spinner"))
	require.True(t, m.Store.Locked("spinner"))
	require.True(t, m.Themes.IsLua("lunar"))

	// A config.lua edit alone (config.yaml unchanged) is seen by the poll.
	writeFile(t, lua, "rex.set('spinner', 'pulse')\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "pulse", m.Store.Get("spinner"))
	require.False(t, m.Themes.IsLua("lunar"))
}

func TestConfigReloadBrokenConfigLuaKeepsLastGoodWithAFlatNotice(t *testing.T) {
	m, cfg, dir, lua := luaModel(t)
	writeFile(t, lua, "rex.set('spinner', 'moon')\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "moon", m.Store.Get("spinner"))

	writeFile(t, lua, `error("\27[2J\27]0;x\7`+strings.Repeat("y", 400)+`")`)
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "moon", m.Store.Get("spinner"), "last good values stay")
	require.True(t, strings.HasPrefix(m.Err, "config.lua: "), m.Err)
	require.NotContains(t, m.Err, "\x1b")
	require.NotContains(t, m.Err, "\x07")
	require.LessOrEqual(t, utf8.RuneCountInString(m.Err), 200)
}

func TestConfigReloadLuaConfigOffNeverTouchesConfigLua(t *testing.T) {
	m, cfg, dir, lua := luaModel(t)
	writeFile(t, lua, "rex.set('spinner', 'moon')\n")
	writeFile(t, cfg, "lua_config: false\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "braille", m.Store.Get("spinner"))
	before := luacfg.Touches()
	writeFile(t, lua, "rex.set('spinner', 'pulse')\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	_, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, true))
	require.Equal(t, before, luacfg.Touches(), "off: no stat, read or eval")
}

func bootLuaSteps(t *testing.T, m Model) (Model, bootLine) {
	t.Helper()
	next, _ := m.Update(stepSettingsLoad(m)())
	m = next.(Model)
	next, _ = m.Update(stepLuaConfig(m)())
	m = next.(Model)
	return m, m.BootLog[len(m.BootLog)-1]
}

func TestBootConfigLuaRow(t *testing.T) {
	m, _, _, lua := luaModel(t)

	_, row := bootLuaSteps(t, Model{})
	require.Equal(t, "config.lua", row.Name)
	require.Equal(t, stepSkip, row.Status)

	writeFile(t, lua, "rex.set('spinner', 'moon')\nrex.theme('lunar', {})\nrex.set('nope', 1)\n")
	luacfg.ResetMemo()
	booted, row := bootLuaSteps(t, Model{})
	require.Equal(t, stepWarn, row.Status, "a warning shows")
	require.Contains(t, row.Desc, "1 setting")
	require.Equal(t, "moon", booted.Store.Get("spinner"))
	_, trow := bootThemeStep(t, booted)
	require.Contains(t, trow.Desc, "lunar")

	writeFile(t, lua, "rex.set('spinner', 'moon')\n")
	luacfg.ResetMemo()
	_, row = bootLuaSteps(t, Model{})
	require.Equal(t, stepOK, row.Status)

	writeFile(t, lua, "rex.set(")
	luacfg.ResetMemo()
	booted, row = bootLuaSteps(t, Model{})
	require.Equal(t, stepWarn, row.Status)
	require.Contains(t, row.Desc, "config.lua")
	require.Contains(t, booted.Err, "config.lua: ")

	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(lua), "config.yaml"), []byte("lua_config: false\n"), 0o600))
	luacfg.ResetMemo()
	before := luacfg.Touches()
	_, row = bootLuaSteps(t, Model{})
	require.Equal(t, stepSkip, row.Status)
	require.Contains(t, row.Desc, "off")
	require.Equal(t, before, luacfg.Touches())
	_ = m
}

// config.lua sets theme auto next to its own themes: auto resolves by the
// boot-time background (paper on a light terminal), and the Lua theme is
// still there to switch to.
func TestConfigReloadLuaAutoTheme(t *testing.T) {
	m, cfg, dir, lua := luaModel(t)
	m.termLight = true
	writeFile(t, lua, "rex.theme('lunar', { extends = 'noir' })\nrex.set('theme', 'auto')\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Empty(t, m.Err)
	require.Equal(t, "auto", m.Store.Get("theme"))
	require.True(t, m.Store.Locked("theme"))
	require.True(t, m.Themes.IsLua("lunar"))
	require.Equal(t, theme.Builtin["paper"].Accent, borderPalette(m).Accent, "auto is paper on a light terminal")

	m.termLight = false
	require.Equal(t, theme.Builtin[theme.DefaultName].Accent, borderPalette(m).Accent)

	writeFile(t, lua, "rex.theme('lunar', { extends = 'noir', colors = { accent = '#B392F0' } })\nrex.set('theme', 'lunar')\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "#B392F0", borderPalette(m).Accent)
}
