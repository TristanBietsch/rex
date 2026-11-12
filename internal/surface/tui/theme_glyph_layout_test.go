package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// A theme with one-cell custom glyphs keeps every row exactly the board
// width, never wraps, at narrow and full width, and shows the glyphs.
func TestCustomOneCellGlyphsKeepRowsAligned(t *testing.T) {
	m := themeModel(t)
	m.Themes = userThemes(t, map[string]string{"g.yaml": `
glyphs: {needs: "!", done: "+", failed: "x", crashed: "o", queued: ".", select: ">"}
`})
	m, _ = executeCommand(m, "theme g")
	require.Empty(t, m.Err)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, w := range layoutWidths {
		lm := layoutModel(now, w, layoutSessions(now)...)
		lm.Store, lm.Themes = m.Store, m.Themes
		cols := boardLayout(lm, w)
		sawSelect, sawNeeds := false, false
		for _, s := range lm.Sessions {
			row := renderRow(lm, s, cols)
			require.NotContains(t, row, "\n", "width %d row %s", w, s.ID)
			require.Equal(t, w, ansi.StringWidth(row), "width %d row %s", w, s.ID)
			plain := ansi.Strip(row)
			if s.ID == lm.SelectedID {
				sawSelect = plain[:3] == " > " || plain[:2] == "> " || plain[1] == '>'
			}
			if s.ID == "1" {
				sawNeeds = plain[rowIndent] == '!' || plain[rowIndent+1] == '!'
			}
		}
		require.True(t, sawSelect, "select glyph on the selected row, width %d", w)
		require.True(t, sawNeeds, "needs glyph on the needs row, width %d", w)
	}
}
