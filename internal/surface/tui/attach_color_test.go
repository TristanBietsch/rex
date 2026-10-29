package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestAttachGlyphRow_TrueColorCells(t *testing.T) {
	// vt10x stores 38;2;r;g;b as r<<16|g<<8|b; read as a 256-color index it
	// rendered an invalid "38;5;1193046" and crashed 16-color profiles.
	out := "\x1b[38;2;18;52;86mX\x1b[48;2;255;0;0mY\x1b[0m"
	withTrueColor(t)
	_, row := termRow(t, 2, out)
	require.Contains(t, row, "38;2;18;52;86")
	require.Contains(t, row, "48;2;255;0;0")

	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, p := range []termenv.Profile{termenv.ANSI256, termenv.ANSI} {
		lipgloss.SetColorProfile(p)
		require.NotPanics(t, func() { termRow(t, 2, out) }, "profile %v", p)
	}
	require.Equal(t, lipgloss.Color("#123456"), vtColorOf(vt10x.Color(0x123456)))
	require.Equal(t, lipgloss.Color("196"), vtColorOf(vt10x.Color(196)))
}

// vtColorOf is vtColor's color, "" for a terminal default.
func vtColorOf(c vt10x.Color) lipgloss.Color {
	col, _ := vtColor(c)
	return col
}
