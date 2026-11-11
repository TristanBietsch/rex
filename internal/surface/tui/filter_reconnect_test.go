package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The daemon's snapshots always say filter "all"; none of the places that
// take a snapshot may reset the user's tool filter.
func TestSnapshotsKeepToolFilter(t *testing.T) {
	snap := &protocol.Snapshot{Filter: "all", Sessions: filterModel().Sessions}
	raw, err := json.Marshal(snap)
	require.NoError(t, err)

	t.Run("reconnect", func(t *testing.T) {
		m := filterModel()
		m.Filter = "claude"
		m = adoptConnection(m, nil, snap)
		require.Equal(t, "claude", m.Filter)
	})
	t.Run("snapshot event", func(t *testing.T) {
		m := filterModel()
		m.Filter = "claude"
		m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: raw})
		require.Equal(t, "claude", m.Filter)
	})
	t.Run("boot handshake", func(t *testing.T) {
		m := filterModel()
		m.Focus = FocusBoot
		m.Filter = "claude"
		next, _ := m.dispatch(snapshotResultMsg{Snap: snap})
		require.Equal(t, "claude", next.(Model).Filter)
	})
	t.Run("adopted while unset", func(t *testing.T) {
		m := filterModel()
		m.Filter = ""
		m = adoptConnection(m, nil, snap)
		require.Equal(t, "all", m.Filter)
	})
}
