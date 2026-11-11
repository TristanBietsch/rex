package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// With the timestamps feature off the detailed view has no span column and
// rows still fill the width; the rest of the row is unchanged.
func TestBoardSpanColumnOffIsInert(t *testing.T) {
	m := spanModel(160, true)
	require.Positive(t, boardLayout(m, 160).span)
	require.NoError(t, m.Store.Set("timestamps", false))
	require.Zero(t, boardLayout(m, 160).span)
	for _, row := range boardRows(t, m, 160) {
		require.NotContains(t, row, "→")
	}
	for _, w := range layoutWidths {
		cols := boardLayout(m, w)
		for _, s := range m.Sessions {
			require.Equal(t, w, ansi.StringWidth(renderRow(m, s, cols)), "width %d row %s", w, s.ID)
		}
	}
}
