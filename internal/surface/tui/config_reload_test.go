package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// reloadModel is a booted Model whose config.yaml and themes/ live in a temp
// dir; it returns the paths so a test can edit them.
func reloadModel(t *testing.T) (m Model, cfg, themeDir string) {
	t.Helper()
	t.Cleanup(func() { applyTheme("default") })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	themeDir = themesDir()
	require.NotEmpty(t, themeDir)
	cfg = filepath.Join(filepath.Dir(themeDir), "config.yaml")
	require.NoError(t, os.MkdirAll(themeDir, 0o700))
	m = Model{Store: settings.NewStore(), StorePath: cfg, Focus: FocusBoard, idleDone: make(chan struct{}), mouseOn: true}
	m.configStamp = configStamp(cfg, themeDir, true)
	return m, cfg, themeDir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	// Stamps compare size and mtime: make an edit within the same clock
	// tick visible.
	later := time.Now().Add(time.Duration(len(body)+1) * time.Hour)
	require.NoError(t, os.Chtimes(path, later, later))
}

// run executes a Cmd the way the runtime would and feeds the message back to
// Update.
func runConfigCmd(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd)
	next, cmd := m.dispatch(cmd())
	return next.(Model), cmd
}

func TestConfigReloadAppliesThemeFile(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	writeFile(t, filepath.Join(dir, "mine.yaml"), "colors: {accent: \"#FF7A59\"}\nglyphs: {needs: \"!\"}\n")
	writeFile(t, cfg, "theme: mine\n")

	m, cmd := runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.NotNil(t, cmd, "the poll continues")
	require.Empty(t, m.Err)
	require.Equal(t, "mine", m.Store.Get("theme"))
	require.Equal(t, lipgloss.Color("#FF7A59"), colorAccent)
	require.Equal(t, "!", stateGlyph("needs_input"))

	// Editing only the theme file restyles without any settings change.
	writeFile(t, filepath.Join(dir, "mine.yaml"), "colors: {accent: \"#00FF00\"}\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, lipgloss.Color("#00FF00"), colorAccent)
	require.Equal(t, "⚑", stateGlyph("needs_input"))

	// Removing the file falls back to default with a note.
	require.NoError(t, os.Remove(filepath.Join(dir, "mine.yaml")))
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Contains(t, m.Err, `theme "mine" not found`)
	require.NotEqual(t, lipgloss.Color("#00FF00"), colorAccent)
}

func TestConfigReloadNoChangeKeepsStoreAndContinues(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	before := m.Store
	m, cmd := runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.NotNil(t, cmd, "re-armed")
	require.Same(t, before, m.Store, "nothing changed: nothing swapped")
}

func TestConfigReloadAppliesOnlyLiveSettings(t *testing.T) {
	old := settings.NewStore()
	cur := settings.NewStore()
	require.NoError(t, cur.Set("spinner", "moon"))               // live
	require.NoError(t, cur.Set("default_spawn_tool", "echo"))    // next spawn
	require.NoError(t, cur.Set("attach_scrollback_lines", 1000)) // next attach
	require.NoError(t, cur.Set("board_hidden_sections", "done")) // live
	require.ElementsMatch(t, []string{"spinner", "board_hidden_sections"}, changedLiveSettings(old, cur))
	require.Empty(t, changedLiveSettings(cur, cur))
}

func TestConfigReloadAppliesBoardHiddenSections(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	writeFile(t, cfg, "board_hidden_sections: done\n")
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Equal(t, "done", m.Store.Get("board_hidden_sections"))
	require.True(t, m.Hidden["done"])
}

func TestConfigReloadKeepsPreviousOnError(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	require.NoError(t, m.Store.Set("spinner", "moon"))
	before := m.Store
	writeFile(t, cfg, "spinner: [unclosed\n")

	m, cmd := runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.NotNil(t, cmd)
	require.Same(t, before, m.Store)
	require.Contains(t, m.Err, "config.yaml:")
	require.Contains(t, m.Err, "kept previous settings")

	// The broken file is reported once, not at every poll.
	m.Err = ""
	m, _ = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, false))
	require.Empty(t, m.Err)
}

func TestConfigReloadDropsAResultOvertakenByAnEdit(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	writeFile(t, cfg, "spinner: moon\n")
	msg := loadConfig(cfg, dir, m.configStamp, false)()
	writeFile(t, cfg, "spinner: pulse\n") // lands while the read was in flight
	next, _ := m.dispatch(msg)
	require.Equal(t, "braille", next.(Model).Store.Get("spinner"), "the stale read is not applied")
	// The next poll reads the newer file.
	m2, _ := runConfigCmd(t, next.(Model), loadConfig(cfg, dir, next.(Model).configStamp, false))
	require.Equal(t, "pulse", m2.Store.Get("spinner"))
}

func TestConfigPollIsInertWhileLiveReloadIsOff(t *testing.T) {
	m, cfg, _ := reloadModel(t)
	writeFile(t, cfg, "live_reload: false\n")
	require.NoError(t, m.Store.Set("live_reload", false))

	m, cmd := armConfig(m)
	require.Nil(t, cmd, "no timer is started")
	require.False(t, m.configTicking)

	// A running chain ends at its next tick.
	m.configTicking = true
	next, cmd := m.dispatch(configTickMsg{})
	require.Nil(t, cmd)
	require.False(t, next.(Model).configTicking)

	// Turning it back on restarts the chain.
	require.NoError(t, m.Store.Set("live_reload", true))
	m.configTicking = false
	m, cmd = armConfig(m)
	require.NotNil(t, cmd)
	require.True(t, m.configTicking)
}

func TestConfigPollWaitsOutBootAndNeedsAPath(t *testing.T) {
	m, _, _ := reloadModel(t)
	m.Focus = FocusBoot
	_, cmd := armConfig(m)
	require.Nil(t, cmd)
	m.Focus = FocusBoard
	m.StorePath = ""
	_, cmd = armConfig(m)
	require.Nil(t, cmd)
}

func TestConfigTickPollsAndManualReloadDoesNotContinue(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	m.configTicking = true
	_, cmd := m.dispatch(configTickMsg{})
	require.NotNil(t, cmd, "a tick starts a poll")

	writeFile(t, cfg, "spinner: moon\n")
	m, cmd = runConfigCmd(t, m, loadConfig(cfg, dir, m.configStamp, true))
	require.Nil(t, cmd, "a manual reload does not add a second timer chain")
	require.Equal(t, "moon", m.Store.Get("spinner"))
}

func TestReloadCommandReloadsConfigEvenWithLiveReloadOff(t *testing.T) {
	m, cfg, _ := reloadModel(t)
	require.NoError(t, m.Store.Set("live_reload", false))
	m.reportReloader = func(context.Context, string) (protocol.ReloadReport, error) {
		return protocol.ReloadReport{}, errors.New("no daemon")
	}
	writeFile(t, cfg, "live_reload: false\nspinner: moon\n")

	next, cmd := executeCommand(m, "reload")
	require.NotNil(t, cmd)
	m = next
	msgs := collectMsgs(t, cmd)
	var got *configReloadedMsg
	for _, msg := range msgs {
		if r, ok := msg.(configReloadedMsg); ok {
			got = &r
		}
	}
	require.NotNil(t, got, "the batch includes a config reload")
	require.True(t, got.Manual)
	n, _ := m.dispatch(*got)
	require.Equal(t, "moon", n.(Model).Store.Get("spinner"))
}

func TestConfigStampSeesCreationEditAndRemoval(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	themeDir := filepath.Join(dir, "themes")
	s0 := configStamp(cfg, themeDir, true)
	writeFile(t, cfg, "spinner: moon\n")
	s1 := configStamp(cfg, themeDir, true)
	require.NotEqual(t, s0, s1)
	writeFile(t, cfg, "spinner: moon\nrow_density: roomy\n")
	s2 := configStamp(cfg, themeDir, true)
	require.NotEqual(t, s1, s2)
	require.NoError(t, os.Remove(cfg))
	require.Equal(t, s0, configStamp(cfg, themeDir, true))
}

// collectMsgs runs cmd and every command inside a batch, returning the
// messages they produce.
func collectMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		default:
			out = append(out, msg)
		}
	}
	walk(cmd)
	return out
}

func TestUpdateStartsTheConfigPollOnceBooted(t *testing.T) {
	m, _, _ := reloadModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	require.True(t, next.(Model).configTicking, "Update arms the poll after boot")

	m.idleDone = nil // no exit signal: the timer could never be stopped
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	require.False(t, next.(Model).configTicking)
}

func TestConfigTickEndsWhenTheTUIExits(t *testing.T) {
	done := make(chan struct{})
	close(done)
	require.Nil(t, configTick(done)(), "a closed exit signal ends the timer at once")
}

// A reload that flips window_border re-lays the board out (the frame takes a
// cell on each side), and window_border_color changes the frame's color,
// without any other event.
func TestConfigReloadRelaysOutOnWindowBorderChange(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	m.TermW, m.TermH, m.Width, m.Height = 100, 30, 100, 30

	writeFile(t, cfg, "window_border: true\n")
	next, _ := m.Update(loadConfig(cfg, dir, m.configStamp, false)())
	m = next.(Model)
	require.Equal(t, 98, m.Width, "the frame takes a cell on each side")
	require.Equal(t, 28, m.Height)

	writeFile(t, cfg, "window_border: true\nwindow_border_color: \"#ff0000\"\n")
	next, _ = m.Update(loadConfig(cfg, dir, m.configStamp, false)())
	m = next.(Model)
	require.Equal(t, "#ff0000", borderColorChoice(m), "the new color is in force")
	require.Equal(t, 98, m.Width)

	writeFile(t, cfg, "window_border: false\n")
	next, _ = m.Update(loadConfig(cfg, dir, m.configStamp, false)())
	m = next.(Model)
	require.Equal(t, 100, m.Width, "the frame is gone and the board is full width again")
	require.Equal(t, 30, m.Height)
}

// The frame's palette comes from the user's theme files too.
func TestBorderPaletteResolvesUserThemes(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{"daylight.yaml": "dark: false\n"})
	require.NoError(t, m.Store.Set("theme", "daylight"))
	require.False(t, borderPalette(m).Dark, "the user theme's own palette")
	require.NoError(t, m.Store.Set("theme", "ghost"))
	require.True(t, borderPalette(m).Dark, "an unknown theme is the default palette")
}

// R20: the Settings page's R reloads everything, like :reload.
func TestSettingsPageReloadKey(t *testing.T) {
	m := settingsScreen(t, 120, 50)
	var calls int
	m.reportReloader = func(context.Context, string) (protocol.ReloadReport, error) {
		calls++
		return protocol.ReloadReport{}, nil
	}
	next, cmd := updateSettingsKey(m, key("R"))
	require.NotNil(t, cmd)
	collectMsgs(t, cmd)
	require.Equal(t, 1, calls)
	require.Equal(t, FocusSettings, next.Focus, "the page stays open")
}

// R20 audit: mouse_enabled is live, so a config.yaml edit (live reload or
// :reload) turns terminal mouse capture off at once, not at the next
// attach or detach.
func TestConfigReloadAppliesMouseEnabled(t *testing.T) {
	m, cfg, dir := reloadModel(t)
	m.mouseOn = true
	writeFile(t, cfg, "mouse_enabled: false\n")
	next, cmd := m.dispatch(loadConfig(cfg, dir, m.configStamp, true)())
	got := next.(Model)
	require.False(t, got.mouseOn, "mouse capture released")
	require.NotNil(t, cmd, "a DisableMouse command is sent")
}

// R20 audit: max_concurrent_sessions (applied from the TUI) is pushed to
// the daemon on a config reload too, as settings.md says.
func TestChangedLiveSettingsIncludesFromTUI(t *testing.T) {
	old, cur := settings.NewStore(), settings.NewStore()
	require.NoError(t, cur.Set("max_concurrent_sessions", 3))
	require.Equal(t, []string{"max_concurrent_sessions"}, changedLiveSettings(old, cur))
}
