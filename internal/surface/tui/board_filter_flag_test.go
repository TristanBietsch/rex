package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// flagOff returns a store with the Features switch id turned off.
func flagOff(t *testing.T, id string) *settings.Store {
	t.Helper()
	st := settings.NewStore()
	require.NoError(t, st.Set(id, false))
	return st
}

// board_filter off is inert: no chip row, no filter chords, no tool cycle,
// the command refuses, and hidden sections are ignored.
func TestBoardFilterOff_IsInert(t *testing.T) {
	off := func() Model {
		m := filterModel()
		m.Store = flagOff(t, "board_filter")
		return m
	}
	t.Run("on by default", func(t *testing.T) {
		require.True(t, filterModel().featureOn("board_filter"))
		require.True(t, showFilterChips(filterModel()), "mixed tools: chip row shows")
	})
	t.Run("no chip row", func(t *testing.T) {
		m := off()
		require.False(t, showFilterChips(m))
		require.Equal(t, 1, headerHeight(m))
		require.Empty(t, headerHitAt(m, 4, 2))
	})
	t.Run("filter command refuses", func(t *testing.T) {
		for _, line := range []string{"filter", "filter claude", "filter hide done", "filter show all", "filter only needs"} {
			m, cmd := executeCommand(off(), line)
			require.Equal(t, "off: enable in Settings → Features (rex config set board_filter true)", m.Err, line)
			require.Nil(t, cmd)
			require.Empty(t, m.Hidden)
			require.Equal(t, filterModel().Filter, m.Filter)
		}
	})
	t.Run("z chords unbound", func(t *testing.T) {
		m := press(off(), "z", "3")
		require.Empty(t, m.PendingChord)
		require.Empty(t, m.Hidden)
		m = press(off(), "z", "h")
		require.Empty(t, m.Hidden)
	})
	t.Run("t unbound", func(t *testing.T) {
		m := press(off(), "t")
		require.Equal(t, filterModel().Filter, m.Filter)
	})
	t.Run("hidden sections ignored", func(t *testing.T) {
		st := flagOff(t, "board_filter")
		require.NoError(t, st.Set("board_hidden_sections", "done"))
		require.Nil(t, hiddenSectionsFrom(st))

		m := off()
		m.Hidden = map[string]bool{"done": true}
		m.Filter = "claude"
		require.Len(t, orderedSessions(m), 4, "stale hidden set and tool filter do not narrow rows")
		require.Empty(t, hiddenStatus(m))
	})
	t.Run("restored state ignored", func(t *testing.T) {
		m := off()
		hidden := []string{"done"}
		next, _ := m.dispatch(stateRestoreResultMsg{OK: true, State: persistedState{Filter: "claude", HiddenSections: &hidden}})
		require.Empty(t, next.(Model).Hidden)
		require.Equal(t, filterModel().Filter, next.(Model).Filter)
	})
}
