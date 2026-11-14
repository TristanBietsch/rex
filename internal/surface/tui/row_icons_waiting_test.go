package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestRowIcons_WaitingSpinner: with the icon strip on, a Waiting row's
// marker turns through the waiting_spinner set in the simple, detailed and
// mobile layouts, with or without ascii_glyphs (which covers the strip and
// legend only, R18 D8), and the legend lists the R18 icons next to the set's
// still glyph.
func TestRowIcons_WaitingSpinner(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		m := waitingSetModel(t, "hourglass", false)
		require.NoError(t, m.Store.Set("row_icons", true))
		require.NoError(t, m.Store.Set("ascii_glyphs", ascii))
		require.True(t, rowIconsOn(m))
		m.SpinnerTick = waitingTicksPerStep // ⧑

		for _, detailed := range []bool{false, true} {
			m.Detailed = detailed
			text := ansi.Strip(renderBoard(m, 120, 0))
			require.Contains(t, text, "⧑", "ascii %v detailed %v: row marker", ascii, detailed)
			require.NotContains(t, text, "◷", "ascii %v detailed %v: no clock", ascii, detailed)
		}
		m.Detailed = false

		require.NoError(t, m.Store.Set("mobile_layout", "always"))
		m.Width = 50
		line1, _ := mobileLines(t, m, 50, "w1")
		require.Contains(t, line1, "⧑", "ascii %v: mobile marker", ascii)
		require.NoError(t, m.Store.Set("mobile_layout", "auto"))

		var keys []string
		for _, r := range helpLegend(m).Rows {
			keys = append(keys, ansi.Strip(r.Key))
		}
		require.Contains(t, keys, "⧖", "ascii %v: legend shows the still glyph", ascii)
		require.NotContains(t, keys, waitingGlyph, "ascii %v", ascii)
		require.Contains(t, keys, iconGlyph(m, "unseen"), "ascii %v: legend keeps the strip icons", ascii)
		require.Contains(t, keys, "sh 42s", "ascii %v: legend keeps the why slot", ascii)
	}
}
