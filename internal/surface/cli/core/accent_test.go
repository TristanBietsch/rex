package core

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// CLI output paints the accent_color override on top of the theme; an
// invalid value is ignored silently, like an unknown theme.
func TestStreamStylesAccent(t *testing.T) {
	var buf bytes.Buffer
	def, _ := theme.Get("default")
	got := streamStyles(&buf, true, true, "default", "#9BB7D4").Accent.GetForeground()
	require.Equal(t, lipgloss.Color("#9BB7D4"), got)
	got = streamStyles(&buf, true, true, "default", "theme").Accent.GetForeground()
	require.Equal(t, lipgloss.Color(def.Accent), got)
	got = streamStyles(&buf, true, true, "default", "rgb(999,0,0)").Accent.GetForeground()
	require.Equal(t, lipgloss.Color(def.Accent), got)
	got = streamStyles(&buf, true, true, "paper", "peach-fuzz").Accent.GetForeground()
	require.GreaterOrEqual(t, theme.Contrast(string(got.(lipgloss.Color)), theme.Builtin["paper"].Bg), theme.AccentMinContrast)
}
