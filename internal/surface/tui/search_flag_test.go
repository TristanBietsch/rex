package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// search off is inert: / unbound, :search refuses, persisted and stale
// queries do not narrow the board.
func TestSearchOff_IsInert(t *testing.T) {
	off := func() Model {
		m := searchModel()
		m.Store = flagOff(t, "search")
		return m
	}
	t.Run("on by default", func(t *testing.T) {
		require.True(t, searchModel().featureOn("search"))
	})
	t.Run("slash unbound", func(t *testing.T) {
		m := press(off(), "/")
		require.Equal(t, FocusBoard, m.Focus)
	})
	t.Run("command refuses", func(t *testing.T) {
		m, cmd := executeCommand(off(), "search auth")
		require.Equal(t, "off: enable in Settings → Features (rex config set search true)", m.Err)
		require.Nil(t, cmd)
		require.Empty(t, m.Search)
		require.Len(t, rowIDs(m), 4)
	})
	t.Run("stale query ignored", func(t *testing.T) {
		m := off()
		m.Search = "auth"
		require.Len(t, rowIDs(m), 4)
		require.Empty(t, searchStatus(m))
	})
	t.Run("restored search ignored", func(t *testing.T) {
		next, _ := off().dispatch(stateRestoreResultMsg{OK: true, State: persistedState{Search: "auth"}})
		require.Empty(t, next.(Model).Search)
		require.Len(t, rowIDs(next.(Model)), 4)
	})
	t.Run("filter command still works", func(t *testing.T) {
		m, _ := executeCommand(off(), "filter claude")
		require.Empty(t, m.Err)
	})
}
