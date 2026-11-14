package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The Done fold hint names the key the board really has: 4 normally, 3
// while the Waiting section is off.
func TestDoneFoldHintFollowsActiveNumber(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	require.Contains(t, ansi.Strip(renderHelpLine(m, 200)), "4 show")
	off := waitingOff(m)
	help := ansi.Strip(renderHelpLine(off, 200))
	require.Contains(t, help, "3 show")
	require.NotContains(t, help, "4 show")
	off, _ = updateKey(off, key("3"))
	require.True(t, off.DoneExpanded, "3 is the Done key with the feature off")
	require.Contains(t, ansi.Strip(renderHelpLine(off, 200)), "3 hide")
}

// A saved "waiting" id means nothing while the section is off: no hidden
// status, no filter bar, and the other sections are untouched.
func TestFlagOff_StaleWaitingIDIsDropped(t *testing.T) {
	m := waitingOff(waitingModel(waiter("w1", "shell", 0)))
	m.Hidden = map[string]bool{"waiting": true}
	require.Empty(t, rowFilterOf(m).hidden)
	require.Empty(t, renderFilterStatus(m))
	require.False(t, showFilterChips(m))

	m.Hidden = map[string]bool{"waiting": true, "done": true}
	require.Equal(t, map[string]bool{"done": true}, rowFilterOf(m).hidden)
	require.Equal(t, "done hidden (zh)", ansi.Strip(renderFilterStatus(m)))
}
