package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// With the timestamps feature off the detailed view has no span column, in
// the fixed (class tables) and the fit layouts, and rows still fill the
// width.
func TestBoardSpanColumnOffIsInert(t *testing.T) {
	for _, layout := range []string{"fixed", columnLayoutFit} {
		m := spanModel(160, true)
		require.NoError(t, m.Store.Set("column_layout", layout))
		require.Positive(t, boardLayout(m, 160).span, layout)
		require.NoError(t, m.Store.Set("timestamps", false))
		require.Zero(t, boardLayout(m, 160).span, layout)
		for _, row := range boardRows(t, m, 160) {
			require.NotContains(t, row, "→", layout)
		}
		for _, w := range layoutWidths {
			cols := boardLayout(m, w)
			require.Zero(t, cols.span, "%s width %d", layout, w)
			for _, s := range m.Sessions {
				require.Equal(t, w, ansi.StringWidth(renderRow(m, s, cols)), "%s width %d row %s", layout, w, s.ID)
			}
		}
	}
}
