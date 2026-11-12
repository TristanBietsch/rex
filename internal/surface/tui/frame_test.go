package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestWindowSizeSetsTermAndLayoutSize(t *testing.T) {
	m := Model{Focus: FocusBoard}
	got, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	nm := got.(Model)
	require.Equal(t, 120, nm.TermW)
	require.Equal(t, 40, nm.TermH)
	require.Equal(t, 120, nm.Width, "no frame: layout is the terminal")
	require.Equal(t, 40, nm.Height)
}

func TestRelayoutKeepsHandSetSizeWithoutTerm(t *testing.T) {
	m := Model{Focus: FocusBoard, Width: 90, Height: 30}
	m = relayout(m)
	require.Equal(t, 90, m.Width)
	require.Equal(t, 30, m.Height)
}

func TestTranslateMouseNoFrameIsIdentity(t *testing.T) {
	m := Model{Focus: FocusBoard, TermW: 80, TermH: 24, Width: 80, Height: 24}
	in := tea.MouseMsg(tea.MouseEvent{X: 5, Y: 7, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	out, ok := translateMouse(m, in)
	require.True(t, ok)
	require.Equal(t, in, out)
}
