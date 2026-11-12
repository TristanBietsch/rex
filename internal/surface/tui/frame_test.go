package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
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

func TestTranslateMouseOnFrame(t *testing.T) {
	m := Model{Focus: FocusBoard, Store: settings.NewStore(), TermW: 120, TermH: 40}
	require.NoError(t, m.Store.Set("window_border", true))
	m = syncLayout(m)
	require.Equal(t, 118, m.Width, "frame shown")
	ev := func(x, y int, a tea.MouseAction) tea.MouseMsg {
		return tea.MouseMsg(tea.MouseEvent{X: x, Y: y, Action: a, Button: tea.MouseButtonLeft})
	}

	_, ok := translateMouse(m, ev(0, 5, tea.MouseActionPress))
	require.False(t, ok, "a press on the frame is dropped")

	// A drag released on the frame still reaches the agent, at the edge.
	out, ok := translateMouse(m, ev(119, 39, tea.MouseActionRelease))
	require.True(t, ok)
	require.Equal(t, m.Width-1, out.X)
	require.Equal(t, m.Height-1, out.Y)

	out, ok = translateMouse(m, ev(0, 0, tea.MouseActionMotion))
	require.True(t, ok)
	require.Equal(t, 0, out.X)
	require.Equal(t, 0, out.Y)
}
