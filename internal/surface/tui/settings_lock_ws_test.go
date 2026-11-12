package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// A row a workspace's .rex/config.lua sets shows the "ws" badge (the same
// width as "lua") and the workspace file and line in the detail.
func TestSettingsLock_WorkspaceBadgeAndDetail(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proj", ".rex", "config.lua")
	rep := settings.LayerReport{Exists: true,
		Values: map[string]any{"spinner": "moon", "theme": "noir"},
		SetAt:  map[string]string{"spinner": file + ":3", "theme": "config.lua:1"}}
	settings.SetLayer(func() settings.LayerReport { return rep })
	t.Cleanup(func() { settings.SetLayer(nil) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	s, _, err := settings.LoadLayered(path)
	require.NoError(t, err)
	m := Model{Store: s, StorePath: path, Width: 100, Height: 40, Focus: FocusSettings, Settings: &SettingsState{CursorID: "spinner"}}

	sp, _ := settings.Find("spinner")
	th, _ := settings.Find("theme")
	require.Equal(t, "ws", ansi.Strip(settingLockBadge(m, sp)))
	require.Equal(t, "lua", ansi.Strip(settingLockBadge(m, th)))
	require.LessOrEqual(t, ansi.StringWidth(settingLockBadge(m, sp)), 3, "fits the room the page keeps for the badge")
	detail := ansi.Strip(strings.Join(settingsDetail(m, sp, 400, 2, false), "\n"))
	require.Contains(t, detail, "set in "+file+":3")

	m, _ = settingsKey(m, keyMsg("enter"))
	require.Contains(t, m.Err, file+":3")
}

func TestLuaFileOf_WorkspaceFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proj", ".rex", "config.lua")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	prev := workspaceConfigFile
	t.Cleanup(func() { workspaceConfigFile = prev })
	workspaceConfigFile = func() string { return file }

	f, line, ok := luaFileOf(file + ":7")
	require.True(t, ok)
	require.Equal(t, file, f)
	require.Equal(t, 7, line)
	other := filepath.Join(t.TempDir(), "other", ".rex", "config.lua")
	for _, bad := range []string{
		"rel/.rex/config.lua:1",
		filepath.Join(t.TempDir(), "x.lua") + ":1",
		other + ":1", // a workspace file this process does not apply
	} {
		_, _, ok := luaFileOf(bad)
		require.False(t, ok, bad)
	}
	workspaceConfigFile = func() string { return "" }
	_, _, ok = luaFileOf(file + ":7")
	require.False(t, ok, "no workspace applies")
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Remove(file))
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "t"), file))
		workspaceConfigFile = func() string { return file }
		_, _, ok = luaFileOf(file + ":7")
		require.False(t, ok, "a symlink is never opened")
	}
}

func TestLuaNotice_NamesTheWorkspaceFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".rex", "config.lua")
	n := luaNotice(settings.LayerReport{Err: errors.New(file + ":1: boom")})
	require.True(t, strings.HasPrefix(n, "workspace config.lua: "), n)
	n = luaNotice(settings.LayerReport{Err: errors.New("config.lua:2: boom")})
	require.True(t, strings.HasPrefix(n, "config.lua: "), n)
}
