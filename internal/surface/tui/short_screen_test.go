package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// On a short terminal the board gives up rows, never the bottom bars: the
// prompt and help line stay on the last two lines and the frame is exactly
// h lines.
func TestRenderFullScreen_ShortScreenPinsBottomBars(t *testing.T) {
	for h := 3; h <= 14; h++ {
		m := simpleBoardStore(t, true)
		lines := strings.Split(ansi.Strip(renderFullScreen(m, 80, h)), "\n")
		require.Len(t, lines, h, "h=%d", h)
		require.Equal(t, ansi.Strip(renderHelpLine(m, 80)), lines[h-1], "h=%d: help line", h)
		require.Equal(t, ansi.Strip(renderPrompt(m, 80)), lines[h-2], "h=%d: prompt", h)
	}
}
