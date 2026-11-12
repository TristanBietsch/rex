package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func staleEnv(t *testing.T, typ string, v any) protocol.Envelope {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return protocol.Envelope{Type: typ, Data: raw}
}

func staleSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "a", Slug: "alpha", ToolID: "claude", State: protocol.StateWorking},
		{ID: "b", Slug: "beta", ToolID: "claude", State: protocol.StateWorking},
	}
}

// Another client deleted the session while its delete confirmation was
// open: the confirmation closes instead of sending a delete for a gone id.
func TestDeleteConfirmClosesWhenTargetRemoved(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.Focus, m.PendingDeleteID = FocusConfirmDelete, "a"
	m = m.applyEvent(staleEnv(t, protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "a"}))
	require.Equal(t, FocusBoard, m.Focus)
	require.Empty(t, m.PendingDeleteID)
	require.Equal(t, "already deleted", m.Notice)
}

func TestDeleteConfirmStaysForOtherRemoval(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.Focus, m.PendingDeleteID = FocusConfirmDelete, "a"
	m = m.applyEvent(staleEnv(t, protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "b"}))
	require.Equal(t, FocusConfirmDelete, m.Focus)
	require.Equal(t, "a", m.PendingDeleteID)
}

// A snapshot (reconnect, or a server resync) that no longer lists the
// targets closes them the same way SessionRemoved would.
func TestSnapshotDropsStaleTargets(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.Focus, m.PendingDeleteID = FocusConfirmDelete, "a"
	m.Attach = &AttachState{SessionID: "b"}
	m.SelectedID = "a"
	snap := protocol.Snapshot{Sessions: []protocol.SessionSummary{
		{ID: "c", Slug: "gamma", ToolID: "claude", State: protocol.StateWorking},
	}}
	m = m.applyEvent(staleEnv(t, protocol.EventSnapshot, snap))
	require.Equal(t, FocusBoard, m.Focus)
	require.Empty(t, m.PendingDeleteID)
	require.True(t, m.Attach.Ended)
	require.Equal(t, "session deleted", m.Attach.EndedMsg)
	require.Equal(t, "c", m.SelectedID, "selection moves off the gone row")
}

func TestSnapshotDropsStaleRename(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.Focus, m.Rename = FocusRename, &RenameState{SessionID: "a"}
	snap := protocol.Snapshot{Sessions: staleSessions()[1:]}
	m = m.applyEvent(staleEnv(t, protocol.EventSnapshot, snap))
	require.Nil(t, m.Rename)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestSnapshotKeepsLiveTargets(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.Focus, m.Rename = FocusRename, &RenameState{SessionID: "a"}
	m.Attach = &AttachState{SessionID: "b"}
	m = m.applyEvent(staleEnv(t, protocol.EventSnapshot, protocol.Snapshot{Sessions: staleSessions()}))
	require.NotNil(t, m.Rename)
	require.Equal(t, FocusRename, m.Focus)
	require.False(t, m.Attach.Ended)
}

// A SessionAdded for a row a snapshot already brought in updates the row
// instead of listing it twice.
func TestSessionAddedTwiceListsOnce(t *testing.T) {
	m := boardModel(staleSessions()...)
	m = m.applyEvent(staleEnv(t, protocol.EventSessionAdded,
		protocol.SessionSummary{ID: "a", Slug: "alpha2", ToolID: "claude", State: protocol.StateDone}))
	require.Len(t, m.Sessions, 2)
	s, ok := findSession(m.Sessions, "a")
	require.True(t, ok)
	require.Equal(t, "alpha2", s.Slug)
}

// The spawn this TUI waits to select can arrive in a snapshot before its
// own add event; the add event still selects it and clears the pending slug.
func TestSessionAddedTwiceStillSelectsPendingSpawn(t *testing.T) {
	m := boardModel(staleSessions()...)
	m.SelectedID = "a"
	m.PendingSelectSlug = "gamma"
	fresh := protocol.SessionSummary{ID: "c", Slug: "gamma", ToolID: "claude", State: protocol.StateWorking}
	m = m.applyEvent(staleEnv(t, protocol.EventSnapshot, protocol.Snapshot{Sessions: append(staleSessions(), fresh)}))
	m = m.applyEvent(staleEnv(t, protocol.EventSessionAdded, fresh))
	require.Len(t, m.Sessions, 3)
	require.Equal(t, "c", m.SelectedID)
	require.Empty(t, m.PendingSelectSlug)
}
