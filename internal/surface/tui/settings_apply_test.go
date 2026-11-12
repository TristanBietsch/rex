package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// board_hidden_sections is applied by the settings page at once, through the
// same reader the board uses at startup (and its board_filter switch).
func TestBoardHiddenSectionsAppliesLive(t *testing.T) {
	m := settingsScreen(t, 120, 40)
	require.NoError(t, m.Store.Set("board_hidden_sections", "done"))
	applyLive(&m, "board_hidden_sections", m.Store.Get("board_hidden_sections"))
	require.True(t, m.Hidden["done"], "the Done section is hidden now")

	require.NoError(t, m.Store.Set("board_hidden_sections", ""))
	applyLive(&m, "board_hidden_sections", "")
	require.Empty(t, m.Hidden)

	require.NoError(t, m.Store.Set("board_filter", false))
	require.NoError(t, m.Store.Set("board_hidden_sections", "done"))
	applyLive(&m, "board_hidden_sections", "done")
	require.Empty(t, m.Hidden, "board_filter off: the setting is ignored, as at startup")
}
