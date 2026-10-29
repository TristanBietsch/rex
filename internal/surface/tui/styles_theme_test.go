package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

func TestApplyTheme_ColorsPromptArrows(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for name, p := range palettes {
		applyTheme(name)
		require.Equal(t, lipgloss.TerminalColor(p.Working), styleArrow.GetForeground(), name)
		require.Equal(t, lipgloss.TerminalColor(p.Needs), styleArrowYellow.GetForeground(), name)
	}
}
