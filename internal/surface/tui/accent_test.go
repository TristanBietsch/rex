package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// accentModel is themeModel with the accent override reset after the test.
func accentModel(t *testing.T) Model {
	t.Helper()
	m := themeModel(t)
	t.Cleanup(func() { accentChoice = ""; applyTheme("default") })
	return m
}

// requireAccent checks every accent-derived style paints c.
func requireAccent(t *testing.T, c string) {
	t.Helper()
	want := lipgloss.Color(c)
	require.Equal(t, want, colorAccent)
	for name, st := range map[string]lipgloss.Style{
		"styleArrow": styleArrow, "styleAccent": styleAccent, "styleCmd": styleCmd, "styleKbd": styleKbd,
	} {
		require.Equal(t, want, st.GetForeground(), name)
	}
}

func TestAccentDefaultKeepsTheTheme(t *testing.T) {
	m := accentModel(t)
	applyLive(&m, "accent_color", "theme")
	requireAccent(t, theme.Builtin["default"].Accent)
	require.Empty(t, m.Notice)
	require.Empty(t, m.Err)
}

func TestAccentAppliesLiveOnDarkAndLight(t *testing.T) {
	m := accentModel(t)
	m = setAccentSetting(m, "cerulean")
	require.Empty(t, m.Err)
	requireAccent(t, "#9BB7D4") // readable on default as is
	require.Empty(t, m.Notice)

	// The override follows a theme change, with the light variant and
	// the contrast fix for paper.
	m = setThemeSetting(m, "paper")
	paper := theme.Builtin["paper"]
	require.Equal(t, lipgloss.Color(paper.Working), colorWorking, "the theme applied")
	require.NotEqual(t, lipgloss.Color(paper.Accent), colorAccent, "the override stays on")
	require.GreaterOrEqual(t, theme.Contrast(string(colorAccent), paper.Bg), theme.AccentMinContrast)
	require.Contains(t, m.Notice, "adjusted to "+string(colorAccent)+" for contrast on paper")

	saved := settings.NewStore()
	require.NoError(t, saved.Load(m.StorePath))
	require.Equal(t, "cerulean", saved.Get("accent_color"))
}

func TestAccentOnUserAndLuaThemes(t *testing.T) {
	m := accentModel(t)
	lunar, _, err := theme.Parse("lunar", []byte("extends: noir\ncolors:\n  accent: \"#123456\"\n"))
	require.NoError(t, err)
	m.Themes = (&theme.Catalog{}).WithLua(map[string]theme.Palette{"lunar": lunar}, nil)
	require.NoError(t, m.Store.Set("theme", "lunar"))
	applyLive(&m, "theme", "lunar")
	requireAccent(t, "#123456")
	m = setAccentSetting(m, "#FF6F61")
	requireAccent(t, "#FF6F61")
	for _, bundled := range []string{"dracula", "tokyo_night", "pantone_safe"} {
		m = setThemeSetting(m, bundled)
		requireAccent(t, "#FF6F61")
	}
}

func TestAccentClashNotice(t *testing.T) {
	m := accentModel(t)
	m = setAccentSetting(m, theme.Builtin["default"].Working)
	require.Contains(t, m.Notice, "close to the working state color")
}

func TestAccentCommand(t *testing.T) {
	m := accentModel(t)
	m, _ = executeCommand(m, "accent rgb(255, 111, 97)")
	require.Empty(t, m.Err)
	require.Equal(t, "rgb(255, 111, 97)", m.Store.Get("accent_color"), "inner spacing kept")
	requireAccent(t, "#FF6F61")

	m, _ = executeCommand(m, "accent theme")
	requireAccent(t, theme.Builtin["default"].Accent)
	m, _ = executeCommand(m, "accent")
	require.Equal(t, theme.AccentIDs()[0], m.Store.Get("accent_color"), "no value: the next name")

	m, _ = executeCommand(m, "accent nope")
	require.Contains(t, m.Err, "unknown accent color")
	require.Equal(t, theme.AccentIDs()[0], m.Store.Get("accent_color"), "refused: unchanged")
}

// A hand-edited config.yaml can hold a value Normalize would refuse: the
// theme's accent is used and the boot log says why.
func TestAccentInvalidStoredValueAtBoot(t *testing.T) {
	m := accentModel(t)
	require.NoError(t, os.WriteFile(m.StorePath, []byte("accent_color: \"rgb(999, 0, 0)\"\n"), 0o600))
	require.NoError(t, m.Store.Load(m.StorePath))
	next, _ := m.Update(themeApplyMsg{Scheme: "default"})
	requireAccent(t, theme.Builtin["default"].Accent)
	log := next.(Model).BootLog
	step := log[len(log)-1]
	require.Equal(t, stepWarn, step.Status)
	require.Contains(t, step.Desc, "accent_color:")
	require.Contains(t, step.Desc, "out of range")
}

func TestAccentAdjustedAtBoot(t *testing.T) {
	m := accentModel(t)
	require.NoError(t, m.Store.Set("accent_color", "ultra-violet"))
	next, _ := m.Update(themeApplyMsg{Scheme: "default"})
	step := next.(Model).BootLog[len(next.(Model).BootLog)-1]
	require.Equal(t, stepOK, step.Status)
	require.Contains(t, step.Desc, "adjusted to")
}

// accentPage is the settings page on the accent row.
func accentPage(t *testing.T) Model {
	t.Helper()
	m := accentModel(t)
	m.Width, m.Height, m.Focus = 120, 40, FocusSettings
	m.Settings = &SettingsState{CursorID: "accent_color"}
	return m
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestAccentPageArrowsCycle(t *testing.T) {
	m := accentPage(t)
	m = applySettingsAction(m, +1)
	require.Equal(t, theme.AccentIDs()[0], m.Store.Get("accent_color"))
	m = applySettingsAction(m, -1)
	require.Equal(t, "theme", m.Store.Get("accent_color"), "theme comes first")
	m = applySettingsAction(m, -1)
	ids := theme.AccentIDs()
	require.Equal(t, ids[len(ids)-1], m.Store.Get("accent_color"))
	requireAccent(t, string(colorAccent)) // styles rebuilt together
	require.NotEqual(t, lipgloss.Color(theme.Builtin["default"].Accent), colorAccent)
}

func TestAccentPageEditPreviewAndCancel(t *testing.T) {
	m := accentPage(t)
	m, _ = settingsKey(m, runes("e"))
	require.NotNil(t, m.Settings.Edit, "e opens the editor on a free-form row")
	require.Empty(t, m.Settings.Edit.Text(), "theme is not a color to edit")
	for _, r := range "#ff6f61" {
		m, _ = settingsKey(m, runes(string(r)))
	}
	requireAccent(t, "#FF6F61") // live preview
	require.Equal(t, "theme", m.Store.Get("accent_color"), "not saved yet")
	pane := ansi.Strip(strings.Join(settingsDetail(m, mustFind(t, "accent_color"), 100, 4, false), "\n"))
	require.Contains(t, pane, "→ #FF6F61")

	m, _ = settingsKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, m.Settings.Edit)
	requireAccent(t, theme.Builtin["default"].Accent) // preview dropped
}

func TestAccentPageEditSaveAndRefuse(t *testing.T) {
	m := accentPage(t)
	m, _ = settingsKey(m, runes("#"))
	require.NotNil(t, m.Settings.Edit, "# starts the editor")
	require.Equal(t, "#", m.Settings.Edit.Text())
	m, _ = settingsKey(m, runes("12"))
	pane := ansi.Strip(strings.Join(settingsDetail(m, mustFind(t, "accent_color"), 100, 4, false), "\n"))
	require.Contains(t, pane, "not #rgb or #rrggbb", "the draft's problem shows while typing")
	m, _ = settingsKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, m.Settings.Edit, "a refused save keeps the editor open")
	require.Contains(t, m.Settings.Edit.Err, "not #rgb or #rrggbb")

	m, _ = settingsKey(m, runes("3"))
	m, _ = settingsKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, "#123", m.Store.Get("accent_color"))
	require.GreaterOrEqual(t, theme.Contrast(string(colorAccent), theme.Builtin["default"].Bg), theme.AccentMinContrast)
	require.Contains(t, m.Notice, "adjusted to", "#112233 is too dark on default")
}

func TestAccentSwatch(t *testing.T) {
	m := accentPage(t)
	withProfile(termenv.TrueColor, func() {
		sw, w := accentSwatch(m, false)
		require.Equal(t, "██ ", ansi.Strip(sw))
		require.Equal(t, 3, w)
		require.NoError(t, m.Store.Set("ascii_glyphs", true))
		sw, _ = accentSwatch(m, false)
		require.Equal(t, "## ", ansi.Strip(sw))
	})
	withProfile(termenv.Ascii, func() {
		sw, w := accentSwatch(m, true)
		require.Empty(t, sw, "no color, no swatch")
		require.Zero(t, w)
	})
}
