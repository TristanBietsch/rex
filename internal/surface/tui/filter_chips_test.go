package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// filter_chips off hides the chip row but keeps filtering: t, :filter and a
// click-free header, with the active tool named in the status instead.
func TestFilterChipsOff_HidesRowKeepsFiltering(t *testing.T) {
	off := func() Model {
		m := filterModel()
		m.Store = flagOff(t, "filter_chips")
		return m
	}
	t.Run("on by default", func(t *testing.T) {
		require.True(t, chipsShown(filterModel()))
		require.Equal(t, 2, headerHeight(filterModel()))
	})
	t.Run("no chip row for mixed tools", func(t *testing.T) {
		m := off()
		require.False(t, chipsShown(m))
		require.False(t, showFilterChips(m))
		require.Equal(t, 1, headerHeight(m))
		require.Empty(t, headerHitAt(m, 4, 2))
	})
	t.Run("t still filters, status names the tool", func(t *testing.T) {
		m := press(off(), "t")
		require.NotEqual(t, filterModel().Filter, m.Filter)
		require.True(t, showFilterChips(m), "an active filter keeps the row")
		head := ansi.Strip(renderHeader(m, 100))
		require.Contains(t, head, m.Filter+" only (t)")
		require.NotContains(t, head, " all ", "no chips")
		require.Empty(t, headerHitAt(m, 4, 2), "status only: nothing to click")
	})
	t.Run(":filter still works", func(t *testing.T) {
		m, _ := executeCommand(off(), "filter claude")
		require.Empty(t, m.Err)
		require.Equal(t, "claude", m.Filter)
	})
	t.Run("chips on: no tool status", func(t *testing.T) {
		m, _ := executeCommand(filterModel(), "filter claude")
		require.NotContains(t, ansi.Strip(renderHeader(m, 100)), "only (t)")
	})
}
