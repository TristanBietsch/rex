package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func TestArrowStylesFollowTheme(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for name, p := range palettes {
		applyTheme(name)
		require.Equal(t, p.Working, styleArrow.GetForeground(), "%s: arrow uses the working color", name)
		require.Equal(t, p.Needs, styleArrowYellow.GetForeground(), "%s: yellow arrow uses the needs color", name)
	}
	require.NotEqual(t, "", string(palettes["default"].Working))
}

func TestStepThemeApply_DoesNotTouchGlobalsOffGoroutine(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	applyTheme("default")
	before := colorWorking
	require.NotEqual(t, palettes["paper"].Working, before, "test needs two distinct palettes")

	store := settings.NewStore()
	require.NoError(t, store.Set("color_scheme", "paper"))
	msg := stepThemeApply(Model{Store: store})()
	require.Equal(t, before, colorWorking, "the Cmd goroutine must not mutate the palette")

	tm, ok := msg.(themeApplyMsg)
	require.True(t, ok, "got %T", msg)
	require.Equal(t, "paper", tm.Scheme)

	next, _ := Model{}.Update(msg)
	require.Equal(t, palettes["paper"].Working, colorWorking, "Update applies the theme")
	log := next.(Model).BootLog
	require.Equal(t, "theme.apply", log[len(log)-1].Name)
	require.Equal(t, stepOK, log[len(log)-1].Status)
}

func TestStepThemeApply_NoSchemeSkips(t *testing.T) {
	msg := stepThemeApply(Model{})()
	tm, ok := msg.(themeApplyMsg)
	require.True(t, ok)
	require.Empty(t, tm.Scheme)
}

func TestCursorVisibleUnderAscii(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.Ascii)
	lit := cursorBlock(Model{SpinnerTick: 0})
	off := cursorBlock(Model{SpinnerTick: 5})
	require.NotEqual(t, lit, off, "lit and off phases must differ without color")
	require.NotEqual(t, "", strings.TrimSpace(lit), "lit cursor is a visible glyph")
}

func TestCursorReverseVideoUnderColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.ANSI256)
	require.Contains(t, cursorBlock(Model{SpinnerTick: 0}), "\x1b[7m")
	require.NotContains(t, cursorBlock(Model{SpinnerTick: 5}), "\x1b[7m")
}
