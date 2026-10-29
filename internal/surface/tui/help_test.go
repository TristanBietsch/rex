package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func helpView(t *testing.T, m Model) string {
	t.Helper()
	v := ansi.Strip(m.View())
	require.Len(t, strings.Split(v, "\n"), m.Height)
	return v
}

func TestHelp_ShortScreenScrollsWithFooterVisible(t *testing.T) {
	for _, h := range []int{24, 40} {
		m := hostileModel(100, h)
		m.Focus = FocusHelp
		v := helpView(t, m)
		require.Contains(t, v, "esc to close", "h=%d", h)
		require.Contains(t, v, "╰", "h=%d: bottom border", h)
		require.Contains(t, v, "Navigation", "h=%d", h)
		require.NotContains(t, v, "Long-running badge", "h=%d", h)

		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
		v = helpView(t, m)
		require.Contains(t, v, "▴12m dim", "h=%d: end reachable", h)
		require.Contains(t, v, "esc to close", "h=%d", h)
		require.NotContains(t, v, "Navigation", "h=%d", h)

		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyPgUp})
		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		helpView(t, m)

		m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyEsc})
		require.Equal(t, FocusBoard, m.Focus)
		require.Zero(t, m.HelpScroll, "closing resets the scroll")
	}
}

// ctrl+c backs out of help without its key handler; the next open still
// starts at the top.
func TestHelp_CtrlCResetsScroll(t *testing.T) {
	m := hostileModel(100, 24)
	m.Focus = FocusHelp
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	require.Positive(t, m.HelpScroll)
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	require.Equal(t, FocusBoard, m.Focus)
	require.Zero(t, m.HelpScroll)
}

func TestHelp_TallScreenShowsEverything(t *testing.T) {
	m := hostileModel(100, 80)
	m.Focus = FocusHelp
	v := helpView(t, m)
	require.Contains(t, v, "Navigation")
	require.Contains(t, v, "▴12m dim")
	require.Contains(t, v, "esc to close")
	require.NotContains(t, v, "j/k scroll")
}
