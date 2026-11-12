package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// luaLocked installs a fake config.lua layer that sets vals (each at
// config.lua:<n>, in the order of ids) and returns a store loaded through it
// and its config.yaml path.
func luaLocked(t *testing.T, ids []string, vals ...any) (*settings.Store, string) {
	t.Helper()
	rep := settings.LayerReport{Exists: true, Values: map[string]any{}, SetAt: map[string]string{}}
	for i, id := range ids {
		rep.Values[id] = vals[i]
		rep.SetAt[id] = "config.lua:" + string(rune('1'+i))
	}
	settings.SetLayer(func() settings.LayerReport { return rep })
	t.Cleanup(func() { settings.SetLayer(nil) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	s, _, err := settings.LoadLayered(path)
	require.NoError(t, err)
	return s, path
}

func lockedSettingsModel(t *testing.T, cursor string) Model {
	t.Helper()
	s, path := luaLocked(t, []string{"spinner", "theme"}, "moon", "noir")
	return Model{Store: s, StorePath: path, Width: 100, Height: 40, Focus: FocusSettings, Settings: &SettingsState{CursorID: cursor}}
}

func TestSettingsLockGuardRefusesEveryEdit(t *testing.T) {
	for _, key := range []string{"enter", " ", "right", "left", "l", "h", "+", "-", "r"} {
		m := lockedSettingsModel(t, "spinner")
		m, _ = settingsKey(m, keyMsg(key))
		require.Equal(t, "moon", m.Store.Get("spinner"), key)
		require.Equal(t, "spinner: set in config.lua:1 — press e to open", m.Err, key)
		require.Nil(t, m.Settings.Edit, key)
		_, err := os.Stat(m.StorePath)
		require.True(t, os.IsNotExist(err), "nothing saved (%s)", key)
	}
}

func TestSettingsLockAdjustableAndBadge(t *testing.T) {
	m := lockedSettingsModel(t, "spinner")
	sp, _ := settings.Find("spinner")
	rd, _ := settings.Find("row_density")
	require.False(t, settingAdjustable(m, sp))
	require.True(t, settingAdjustable(m, rd))
	require.Equal(t, "lua", ansi.Strip(settingLockBadge(m, sp)))
	require.Empty(t, settingLockBadge(m, rd))

	detail := ansi.Strip(strings.Join(settingsDetail(m, sp, 200, 2, false), "\n"))
	require.Contains(t, detail, "set in config.lua:1")
	view := ansi.Strip(renderSettings(m, 100))
	require.Contains(t, view, "lua")
	for _, r := range view {
		require.Less(t, r, rune(0x1F000), "no emoji")
	}
}

func TestSettingsUnlockedRowStillEdits(t *testing.T) {
	m := lockedSettingsModel(t, "row_density")
	before := m.Store.Get("row_density")
	m, _ = settingsKey(m, keyMsg("right"))
	require.NotEqual(t, before, m.Store.Get("row_density"))
	require.Empty(t, m.Err)
}

func TestSettingsLockEditKeyOpensEditorAtLine(t *testing.T) {
	m := lockedSettingsModel(t, "theme")
	t.Setenv("EDITOR", "nvim")
	m, cmd := settingsKey(m, keyMsg("e"))
	require.NotNil(t, cmd, "e on a locked row opens the editor")
	require.Empty(t, m.Err)

	unlocked := lockedSettingsModel(t, "row_density")
	_, cmd = settingsKey(unlocked, keyMsg("e"))
	require.Nil(t, cmd, "e does nothing on a row config.lua does not set")
}

func TestLuaEditorArgs(t *testing.T) {
	for _, c := range []struct {
		editor string
		want   []string
	}{
		{"vim", []string{"+12", "/f.lua"}},
		{"/usr/bin/nvim", []string{"+12", "/f.lua"}},
		{"nano", []string{"+12", "/f.lua"}},
		{"emacs", []string{"+12", "/f.lua"}},
		{"micro", []string{"+12", "/f.lua"}},
		{"vi", []string{"+12", "/f.lua"}},
		{`C:\bin\vim.exe`, []string{"+12", "/f.lua"}},
		{"code", []string{"/f.lua"}},
		{"notepad", []string{"/f.lua"}},
	} {
		require.Equal(t, c.want, luaEditorArgs(c.editor, "/f.lua", 12), c.editor)
	}
	require.Equal(t, []string{"/f.lua"}, luaEditorArgs("vim", "/f.lua", 0))
}

func TestLuaFileOf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := filepath.Dir(themesDir())
	f, line, ok := luaFileOf("config.lua:12")
	require.True(t, ok)
	require.Equal(t, filepath.Join(dir, "config.lua"), f)
	require.Equal(t, 12, line)
	f, line, ok = luaFileOf("lua/sub/ui.lua:3")
	require.True(t, ok)
	require.Equal(t, filepath.Join(dir, "lua", "sub", "ui.lua"), f)
	require.Equal(t, 3, line)
	for _, bad := range []string{"", "x.lua:1", "lua/../x.lua:1", "config.lua", "lua/a.lua:x"} {
		_, _, ok := luaFileOf(bad)
		require.False(t, ok, bad)
	}
}

func TestThemeCommandRefusesLockedTheme(t *testing.T) {
	m := lockedSettingsModel(t, "spinner")
	m.Focus = FocusBoard
	m, _ = runTheme(m, cmdCall{Args: []string{"paper"}})
	require.Equal(t, "noir", m.Store.Get("theme"))
	require.Equal(t, "theme: set in config.lua:2 — press e to open", m.Err)
	m.Err = ""
	m, _ = runTheme(m, cmdCall{})
	require.Equal(t, "noir", m.Store.Get("theme"))
	require.Contains(t, m.Err, "config.lua:2")
}

func TestSpawnCascadeRefusesWhenADependentIsLocked(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	s, path := luaLocked(t, []string{"default_spawn_model"}, "opus")
	m := Model{Store: s, StorePath: path, Settings: &SettingsState{Tools: visibleTools(reg.Tools), CursorID: "default_spawn_tool"}}
	m = applySettingsAction(m, +1)
	require.Equal(t, "claude", m.Store.Get("default_spawn_tool"), "a tool change would reset the locked model")
	require.Equal(t, "default_spawn_model: set in config.lua:1 — press e to open", m.Err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestSetupWizardSkipsLockedKeys(t *testing.T) {
	m, cfg := trustWizard(t)
	settings.SetLayer(func() settings.LayerReport {
		return settings.LayerReport{Exists: true,
			Values: map[string]any{"theme": "noir", "auto_trust_dirs": ""},
			SetAt:  map[string]string{"theme": "config.lua:1", "auto_trust_dirs": "config.lua:2"}}
	})
	t.Cleanup(func() { settings.SetLayer(nil) })
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: ~/dev\n"), 0o600))
	msg, ok := m.doWriteCmd()().(setupWriteMsg)
	require.True(t, ok)
	require.NoError(t, msg.err)
	require.ElementsMatch(t, []string{"theme", "auto_trust_dirs"}, msg.locked)
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "theme", "locked keys are not written")
	require.Contains(t, string(raw), "auto_trust_dirs: ~/dev", "nor reset")
	require.Contains(t, string(raw), "spinner")

	next, _ := m.Update(msg)
	view := setupView(next.(SetupWizardModel))
	require.Contains(t, view, "config.lua")
	require.Contains(t, view, "theme")
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	}
	return key(s)
}

// On the settings page a locked row's refusal shows in the footer (the
// board's status line is not drawn behind a page), even with show_help_bar
// off, and the hints offer e.
func TestSettingsPageLockedRowFooter(t *testing.T) {
	m := lockedSettingsModel(t, "spinner")
	require.True(t, pagesOn(m))
	require.Contains(t, ansi.Strip(m.View()), "e open config.lua")
	require.NoError(t, m.Store.Set("show_help_bar", false))
	m, _ = settingsKey(m, keyMsg("h"))
	require.Equal(t, "moon", m.Store.Get("spinner"))
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	require.Contains(t, lines[len(lines)-1], "err: spinner: set in config.lua:1 — press e to open")
}
