package tui

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// A new agent starts at the size openAttach will give its PTY, so the first
// attach needs no resize (a resize would also corrupt agents that lay out
// once, like Antigravity).
func TestSpawnSizeOf_MatchesAttachSize(t *testing.T) {
	for _, dim := range [][2]int{{200, 50}, {120, 40}, {100, 30}, {70, 24}, {45, 30}, {30, 24}} {
		for _, banner := range []string{"static", "off"} {
			for _, mobile := range []string{"auto", "off"} {
				label := fmt.Sprintf("%dx%d banner=%s mobile=%s", dim[0], dim[1], banner, mobile)
				m := Model{Width: dim[0], Height: dim[1], Store: settings.NewStore()}
				require.NoError(t, m.Store.Set("attach_banner", banner), label)
				require.NoError(t, m.Store.Set("mobile_layout", mobile), label)
				wantC, wantR := attachChromeOf(m).innerSize(m.Width, m.Height)
				c, r := spawnSizeOf(m)
				require.Equal(t, [2]int{wantC, wantR}, [2]int{int(c), int(r)}, label)
				require.NotZero(t, c, label)
			}
		}
	}
}

func TestSpawnSizeOf_UnknownTerminalIsDaemonDefault(t *testing.T) {
	c, r := spawnSizeOf(Model{Store: settings.NewStore()})
	require.Zero(t, c)
	require.Zero(t, r)
}

func TestSpawnSessionCmd_SendsSize(t *testing.T) {
	store := settings.NewStore()
	require.NoError(t, store.Set("default_spawn_tool", "claude"))
	require.NoError(t, store.Set("default_spawn_model", "opus"))
	fake := &recordingClient{}
	require.Nil(t, spawnSessionCmd(fake, store, nil, "x", 177, 44)())
	require.NotNil(t, fake.last)
	require.Equal(t, [2]uint16{177, 44}, [2]uint16{fake.last.Cols, fake.last.Rows})
}
