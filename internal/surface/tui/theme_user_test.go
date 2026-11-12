package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func userThemes(t *testing.T, files map[string]string) *theme.Catalog {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	return theme.LoadDir(dir)
}

func TestUserThemeAppliesColorsAndGlyphs(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{"mine.yaml": `
extends: noir
colors: {accent: "#FF7A59"}
glyphs: {needs: "!", done: "+", select: ">"}
`})
	require.Equal(t, append(themeOptions(Model{}), "mine"), themeOptions(m))

	m, _ = executeCommand(m, "theme mine")
	require.Empty(t, m.Err)
	require.Equal(t, "mine", m.Store.Get("theme"))
	require.Equal(t, lipgloss.Color("#FF7A59"), colorAccent)
	require.Equal(t, lipgloss.Color(theme.Builtin["noir"].Working), colorWorking, "the rest comes from the base")
	require.Equal(t, "!", stateGlyph(protocol.StateNeedsInput))
	require.Equal(t, "+", stateGlyph(protocol.StateDone))
	require.Equal(t, "✕", stateGlyph(protocol.StateFailed), "an unset glyph keeps the built-in")
	require.Equal(t, ">", selectGlyph())
	require.Contains(t, stateMarkerCellFor(m, protocol.StateNeedsInput, 0, false), "!")
	require.Contains(t, renderHelpLegendText(m), "!")

	m, _ = executeCommand(m, "theme default")
	require.Equal(t, "◆", stateGlyph(protocol.StateNeedsInput), "leaving the theme restores the markers")
	require.Equal(t, "▸", selectGlyph())
}

func renderHelpLegendText(m Model) string {
	var out string
	for _, r := range helpLegend(m).Rows {
		out += r.Key + " " + r.Desc + "\n"
	}
	return out
}

func TestBadUserThemeFallsBackWithAReason(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{"bad.yaml": `colors: {accent: "red"}`})
	require.Equal(t, themeOptions(Model{}), themeOptions(m), "a skipped file is not offered")

	applyLive(&m, "theme", "bad")
	require.Contains(t, m.Err, `theme "bad" not loaded`)
	require.Contains(t, m.Err, "colors.accent")
	require.Equal(t, lipgloss.Color(theme.Builtin["default"].Accent), colorAccent, "the default palette is in force")
}

func TestUnknownThemeFallsBackWithoutReason(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, nil)
	applyLive(&m, "theme", "ghost")
	require.Equal(t, `theme "ghost" not found (using default)`, m.Err)
}

// A user file named after a bundled theme overrides it in place: the name is
// offered once, and the file's colors win.
func TestThemeOptionsNoDuplicates(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{
		"dracula.yaml": "colors: {accent: \"#123456\"}\n",
		"mine.yaml":    "",
	})
	opts := themeOptions(m)
	n := 0
	for _, o := range opts {
		if o == "dracula" {
			n++
		}
	}
	require.Equal(t, 1, n, "%v", opts)
	require.Equal(t, append(themeOptions(Model{}), "mine"), opts)

	m, _ = executeCommand(m, "theme dracula")
	require.Empty(t, m.Err)
	require.Equal(t, lipgloss.Color("#123456"), colorAccent)
}

// A bundled theme applies with no theme files at all.
func TestBundledThemeApplies(t *testing.T) {
	m := themeModel(t)
	require.Contains(t, themeOptions(m), "tokyo_night")
	m, _ = executeCommand(m, "theme tokyo_night")
	require.Empty(t, m.Err)
	require.Equal(t, lipgloss.Color(theme.Bundled["tokyo_night"].Working), colorWorking)
}
