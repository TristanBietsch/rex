package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestHelpSectionKeysRowFollowsActiveSections(t *testing.T) {
	on := ansi.Strip(renderHelp(Model{}, 140, 80))
	require.Contains(t, on, "1-4")
	require.Contains(t, on, "jump to Needs you / Waiting / Working")

	off := ansi.Strip(renderHelp(waitingOff(Model{}), 140, 80))
	require.Contains(t, off, "1-3")
	require.Contains(t, off, "jump to Needs you / Working / Done (3 also")
	require.NotContains(t, off, "1-4")
	require.NotContains(t, off, "Waiting")
}
