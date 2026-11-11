package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = updateKey(m, key(k))
	}
	return m
}

func TestZChords(t *testing.T) {
	t.Run("z3 toggles Done", func(t *testing.T) {
		m := press(filterModel(), "z", "3")
		require.True(t, m.Hidden["done"])
		require.Empty(t, m.PendingChord)
		m = press(m, "z", "3")
		require.False(t, m.Hidden["done"])
	})
	t.Run("zh toggles completed sections", func(t *testing.T) {
		m := press(filterModel(), "z", "h")
		require.Equal(t, map[string]bool{"done": true}, m.Hidden)
		m = press(m, "z", "h")
		require.Empty(t, m.Hidden)
	})
	t.Run("z0 shows everything", func(t *testing.T) {
		m := press(filterModel(), "z", "1", "z", "3")
		require.Len(t, m.Hidden, 2)
		m = press(m, "z", "0")
		require.Empty(t, m.Hidden)
	})
	t.Run("out of range number gives a notice", func(t *testing.T) {
		m := press(filterModel(), "z", "9")
		require.Equal(t, "no section 9: use z1-z3", m.Notice)
		require.Empty(t, m.Hidden)
	})
	t.Run("z then another key is that key", func(t *testing.T) {
		m := filterModel()
		m.SelectedID = "n1"
		m = press(m, "z")
		require.Equal(t, "z", m.PendingChord)
		m = press(m, "j")
		require.Empty(t, m.PendingChord)
		require.NotEqual(t, "n1", m.SelectedID, "j moved the selection")
	})
	t.Run("hiding every section is refused", func(t *testing.T) {
		m := press(filterModel(), "z", "1", "z", "2", "z", "3")
		require.Equal(t, "filter: can't hide every section", m.Err)
		require.False(t, m.Hidden["done"])
	})
}

func TestHelpLineShowsZHint(t *testing.T) {
	m := press(filterModel(), "z")
	line := ansi.Strip(renderHelpLine(m, 100))
	require.True(t, strings.Contains(line, "z: h completed · 1-9 section · 0 show all"), line)
}
