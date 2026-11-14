package tui

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

func themeModel(t *testing.T) Model {
	t.Helper()
	t.Cleanup(func() { applyTheme("default") })
	return Model{Store: settings.NewStore(), StorePath: filepath.Join(t.TempDir(), "config.yaml")}
}

func TestThemeCommandSetsAndSaves(t *testing.T) {
	m := themeModel(t)
	m, _ = executeCommand(m, "theme paper")
	require.Empty(t, m.Err)
	require.Equal(t, "paper", m.Store.Get("theme"))
	require.Equal(t, lipgloss.Color(theme.Builtin["paper"].Working), colorWorking)

	saved := settings.NewStore()
	require.NoError(t, saved.Load(m.StorePath))
	require.Equal(t, "paper", saved.Get("theme"))
}

func TestThemeCommandCyclesWithoutName(t *testing.T) {
	m := themeModel(t)
	opts := themeOptions(m)
	m, _ = executeCommand(m, "theme")
	require.Equal(t, opts[1], m.Store.Get("theme"))
}

func TestThemeCommandRejectsUnknownAndExtraArgs(t *testing.T) {
	m := themeModel(t)
	m, _ = executeCommand(m, "theme nope")
	require.Contains(t, m.Err, `unknown "nope"`)
	require.Equal(t, "default", m.Store.Get("theme"))
	m.Err = ""
	m, _ = executeCommand(m, "theme a b")
	require.Contains(t, m.Err, "usage")
}

func TestDynamicThemeOptions(t *testing.T) {
	m := themeModel(t)
	opts, ok := dynamicOptions(m, "theme")
	require.True(t, ok)
	require.ElementsMatch(t, append(theme.Names(), theme.AutoName), opts)
	require.Equal(t, "default", opts[0])
}

func TestApplyLiveUnknownThemeNotesAndFallsBack(t *testing.T) {
	m := themeModel(t)
	applyLive(&m, "theme", "ghost")
	require.Contains(t, m.Err, `"ghost" not found`)
	require.Equal(t, lipgloss.Color(theme.Builtin["default"].Working), colorWorking)
}

func TestThemeCommandAutoUsesTheBootResult(t *testing.T) {
	m := themeModel(t)
	m.termLight = true // the boot-time query found a light background
	m, _ = executeCommand(m, "theme auto")
	require.Empty(t, m.Err)
	require.Equal(t, "auto", m.Store.Get("theme"))
	require.Equal(t, lipgloss.Color(theme.Builtin["paper"].Working), colorWorking)
	require.False(t, borderPalette(m).Dark)

	m.termLight = false
	applyLive(&m, "theme", "auto")
	require.Empty(t, m.Err)
	require.Equal(t, lipgloss.Color(theme.Builtin["default"].Working), colorWorking)
	require.True(t, borderPalette(m).Dark)
}
