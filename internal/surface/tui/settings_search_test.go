package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// settingsScreen is the settings page open on a w x h terminal with a
// throwaway config file.
func settingsScreen(t *testing.T, w, h int) Model {
	t.Helper()
	m := Model{
		Focus:     FocusSettings,
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Width:     w, Height: h,
		Settings: &SettingsState{CursorID: settings.Visible()[0].ID},
	}
	return m
}

func settingsPress(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		default:
			msg = key(k)
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func settingsText(m Model) string { return ansi.Strip(m.View()) }

func typeInto(m Model, s string) Model {
	for _, r := range s {
		m = settingsPress(m, string(r))
	}
	return m
}

func TestSettingsSearch(t *testing.T) {
	m := settingsScreen(t, 120, 40)
	before := settingsText(m)
	require.Contains(t, before, "Theme")
	require.Contains(t, before, "Spinner type")

	m = typeInto(settingsPress(m, "/"), "spin")
	require.True(t, m.Settings.Searching)
	text := settingsText(m)
	require.Contains(t, text, "Spinner type")
	require.NotContains(t, text, "Theme")
	require.Equal(t, "spinner", m.Settings.CursorID, "the cursor moves onto the match")
	require.Contains(t, text, "/ spin", "the query shows")

	m = settingsPress(m, "enter")
	require.False(t, m.Settings.Searching)
	require.Equal(t, "spin", m.Settings.Search)
	require.NotContains(t, settingsText(m), "Theme", "the filter outlives the editor")

	m = settingsPress(m, "esc")
	require.Equal(t, FocusSettings, m.Focus, "first esc clears the search")
	require.Empty(t, m.Settings.Search)
	require.Contains(t, settingsText(m), "Theme", "esc restores the list")

	m = settingsPress(m, "esc")
	require.Equal(t, FocusBoard, m.Focus, "second esc closes")
}

func TestSettingsSearchMatchesIDSectionAndHelp(t *testing.T) {
	m := typeInto(settingsPress(settingsScreen(t, 120, 40), "/"), "prompt_glyph")
	require.Contains(t, settingsText(m), "Prompt glyph")

	m = typeInto(settingsPress(settingsScreen(t, 120, 40), "/"), "audio")
	text := settingsText(m)
	require.Contains(t, text, "Soundset")
	require.NotContains(t, text, "Spinner type")

	m = typeInto(settingsPress(settingsScreen(t, 120, 40), "/"), "local model")
	require.Contains(t, settingsText(m), "Summary", "matches help text")

	m = typeInto(settingsPress(settingsScreen(t, 120, 40), "/"), "zzzz")
	require.Contains(t, settingsText(m), "no setting matches")
}

func TestSettingsSearchEditing(t *testing.T) {
	m := typeInto(settingsPress(settingsScreen(t, 120, 40), "/"), "spinx")
	m = settingsPress(m, "backspace")
	require.Equal(t, "spin", m.Settings.Search)
	m = settingsPress(m, "ctrl+u")
	require.Empty(t, m.Settings.Search)
	m = settingsPress(m, "esc")
	require.False(t, m.Settings.Searching)
	require.Equal(t, FocusSettings, m.Focus, "esc ends the editor without closing")
	m = typeInto(settingsPress(m, "/"), "j")
	require.Equal(t, "j", m.Settings.Search, "j types instead of moving")
}

func TestSettingsSectionJump(t *testing.T) {
	m := settingsScreen(t, 120, 40)
	first := func(sec settings.Section) string {
		for _, s := range settings.Visible() {
			if s.Section == sec {
				return s.ID
			}
		}
		return ""
	}
	require.Equal(t, settings.SectionAppearance, mustFind(t, m.Settings.CursorID).Section)

	m = settingsPress(m, "j", "j") // inside Appearance
	m = settingsPress(m, "tab")
	require.Equal(t, first(settings.SectionBehavior), m.Settings.CursorID, "tab: first row of the next section")
	m = settingsPress(m, "tab")
	require.Equal(t, settings.SectionFeatures, mustFind(t, m.Settings.CursorID).Section)

	m = settingsPress(m, "shift+tab")
	require.Equal(t, first(settings.SectionBehavior), m.Settings.CursorID)
	m = settingsPress(m, "j", "shift+tab")
	require.Equal(t, first(settings.SectionBehavior), m.Settings.CursorID, "shift+tab inside a section: its own first row")
	m = settingsPress(m, "shift+tab")
	require.Equal(t, first(settings.SectionAppearance), m.Settings.CursorID)
	m = settingsPress(m, "shift+tab")
	require.Equal(t, first(settings.SectionAdvanced), m.Settings.CursorID, "shift+tab wraps to the last section")

	last := settings.Visible()[len(settings.Visible())-1]
	m.Settings.CursorID = last.ID
	m = settingsPress(m, "tab")
	require.Equal(t, first(settings.SectionAppearance), m.Settings.CursorID, "tab wraps")
}

func mustFind(t *testing.T, id string) settings.Setting {
	t.Helper()
	s, ok := settings.Find(id)
	require.True(t, ok, id)
	return s
}

// A value that differs from the default is marked with a bullet.
func TestModifiedMarker(t *testing.T) {
	m := settingsScreen(t, 120, 60)
	require.NotContains(t, settingsText(m), "•", "a fresh store has no modified setting")

	require.NoError(t, m.Store.Set("spinner", "moon"))
	var rows []string
	for _, l := range strings.Split(settingsText(m), "\n") {
		if strings.Contains(l, "•") {
			rows = append(rows, l)
		}
	}
	require.Len(t, rows, 1)
	require.Contains(t, rows[0], "Spinner type")
	require.Contains(t, rows[0], "• moon")

	require.NoError(t, m.Store.Reset("spinner"))
	require.NotContains(t, settingsText(m), "•")
}

func TestBoolOnIsAccent(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		m := settingsScreen(t, 120, 60)
		out := m.View()
		require.Contains(t, out, styleArrow.Render("on"), "on reads in the accent")
	})
}

// The detail pane under the list names the selected setting's id (for
// `rex config set`), its default, its options and when a change applies.
func TestDetailShowsIDDefaultOptions(t *testing.T) {
	m := settingsScreen(t, 120, 40)
	m.Settings.CursorID = "spinner"
	text := settingsText(m)
	require.Contains(t, text, "id spinner")
	require.Contains(t, text, "default braille")
	require.Contains(t, text, "applies now")
	for _, o := range mustFind(t, "spinner").Options {
		require.Contains(t, text, o, "option %s", o)
	}

	m.Settings.CursorID = "attach_banner"
	require.Contains(t, settingsText(m), "next attach")
	m.Settings.CursorID = "summary_model"
	require.Contains(t, settingsText(m), "after a daemon restart")
	m.Settings.CursorID = "skip_permissions"
	require.Contains(t, settingsText(m), "next new session")
}

func TestDetailHighlightsCurrentOption(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		m := settingsScreen(t, 120, 40)
		require.NoError(t, m.Store.Set("spinner", "moon"))
		m.Settings.CursorID = "spinner"
		out := m.View()
		require.Contains(t, out, styleKey.Render("moon"))
		require.Contains(t, out, styleDim.Render("braille"))
	})
}

// `?` shows the whole help text; by default it is cut to a few lines.
func TestDetailHelpExpands(t *testing.T) {
	m := settingsScreen(t, 70, 60)
	m.Settings.CursorID = "auto_trust_dirs"
	short := settingsText(m)
	require.Contains(t, short, "? more")
	m = settingsPress(m, "?")
	require.True(t, m.Settings.HelpExpanded)
	long := settingsText(m)
	require.Contains(t, long, "apply.")
	require.NotContains(t, short, "apply.")
	require.NotContains(t, long, "? more")
	m = settingsPress(m, "?")
	require.False(t, m.Settings.HelpExpanded)
}

func TestHiddenSettingNotListed(t *testing.T) {
	m := settingsScreen(t, 120, 80)
	require.NotContains(t, settingsText(m), "Keybind preset")
	_, ok := settings.Find("keybind_preset")
	require.True(t, ok)
	seen := map[string]bool{}
	for range 80 {
		seen[m.Settings.CursorID] = true
		m = settingsPress(m, "j")
	}
	require.False(t, seen["keybind_preset"], "navigation skips the hidden row")
	require.True(t, seen["mouse_enabled"])
}

// The cursor stays on a row that is on screen, at every height.
func TestSettingsDetailKeepsSelectedVisible(t *testing.T) {
	for _, h := range []int{16, 20, 24, 30, 40} {
		for _, s := range settings.Visible() {
			m := settingsScreen(t, 100, h)
			m.Settings.CursorID = s.ID
			require.Contains(t, settingsText(m), "▸ "+s.Label, "h=%d id=%s", h, s.ID)
		}
	}
}

func TestSettingsScreenFitsTerminal(t *testing.T) {
	for _, sz := range []goldenSize{{80, 24}, {120, 40}, {50, 20}, {30, 20}} {
		m := settingsScreen(t, sz.w, sz.h)
		for _, l := range strings.Split(m.View(), "\n") {
			require.LessOrEqual(t, ansi.StringWidth(l), sz.w)
		}
		require.Len(t, strings.Split(m.View(), "\n"), sz.h)
	}
}
