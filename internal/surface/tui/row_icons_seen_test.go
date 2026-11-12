package tui

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var seenT0 = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

// seenModel is a board with row_icons on and a movable clock.
func seenModel(t *testing.T, now *time.Time, sessions ...protocol.SessionSummary) Model {
	t.Helper()
	m := iconModel(t, "row_icons", true)
	m.Focus, m.Filter, m.Width, m.Height = FocusBoard, "all", 160, 40
	m.Sessions = sessions
	if len(sessions) > 0 {
		m.SelectedID = sessions[0].ID
	}
	m.Clock = func() time.Time { return *now }
	return m
}

func seenPatch(t *testing.T, m Model, id string, patch map[string]any) Model {
	t.Helper()
	raw, err := json.Marshal(protocol.SessionUpdated{SessionID: id, Patch: patch})
	require.NoError(t, err)
	return m.applyEvent(protocol.Envelope{Type: protocol.EventSessionUpdated, Data: raw})
}

// seenBoot feeds a snapshot the way boot does: the handshake result message.
func seenBoot(t *testing.T, m Model, sessions ...protocol.SessionSummary) Model {
	t.Helper()
	next, _ := m.Update(snapshotResultMsg{Snap: &protocol.Snapshot{Sessions: sessions}})
	return next.(Model)
}

// seenReconnect feeds a snapshot the way a reconnect does.
func seenReconnect(m Model, sessions ...protocol.SessionSummary) Model {
	return adoptConnection(m, nil, &protocol.Snapshot{Sessions: sessions})
}

func TestSeen_ReadyAfterAttachIsUnseen(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	markSeen(&m, "a") // attach at t0
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "needs_input"})
	require.True(t, isUnseen(m, m.Sessions[0]))
	now = seenT0.Add(2 * time.Minute)
	markSeen(&m, "a")
	require.False(t, isUnseen(m, m.Sessions[0]))
}

func TestSeen_DoneAndReadyOnly(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now,
		protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "d", Slug: "d", State: protocol.StateWorking})
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "d", map[string]any{"state": "done"})
	m = seenPatch(t, m, "w", map[string]any{"state": "working", "last_line": "x"})
	require.True(t, isUnseen(m, m.Sessions[1]))
	require.False(t, isUnseen(m, m.Sessions[0]))
	// A second Done patch is not a new entry: seen stays seen.
	now = seenT0.Add(2 * time.Minute)
	markSeen(&m, "d")
	now = seenT0.Add(3 * time.Minute)
	m = seenPatch(t, m, "d", map[string]any{"state": "done"})
	require.False(t, isUnseen(m, m.Sessions[1]))
}

func TestSeen_CCClears(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	m.retirer = &fakeRetirer{}
	markSeen(&m, "a")
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "done"})
	require.True(t, isUnseen(m, m.Sessions[0]))
	now = seenT0.Add(2 * time.Minute)
	next, _ := m.Update(key("c"))
	next, _ = next.(Model).Update(key("c"))
	m = next.(Model)
	require.Equal(t, now, m.SeenAt["a"])
	require.False(t, isUnseen(m, m.Sessions[0]))

	// A marked-done row never shows the mark.
	now = seenT0.Add(3 * time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "needs_input"})
	require.True(t, isUnseen(m, m.Sessions[0]))
	m = seenPatch(t, m, "a", map[string]any{"retire_at": now.Add(time.Hour).Format(time.RFC3339)})
	require.False(t, m.Sessions[0].RetireAt.IsZero())
	require.False(t, isUnseen(m, m.Sessions[0]))
}

func TestSeen_WorkingNeverUnseen(t *testing.T) {
	now := seenT0
	for _, st := range []protocol.State{protocol.StateWorking, protocol.StateQueued} {
		s := protocol.SessionSummary{ID: "a", State: st}
		m := seenModel(t, &now, s)
		m.EnteredAt = map[string]time.Time{"a": seenT0.Add(time.Hour)}
		m.SeenAt = map[string]time.Time{"a": seenT0}
		require.False(t, isUnseen(m, s), st)
	}
	// Waiting (working on a tool) is still not Ready or Done.
	s := protocol.SessionSummary{ID: "a", State: protocol.StateWorking, WaitingOn: protocol.WaitShell,
		WaitingSince: seenT0.Add(-time.Minute)}
	m := seenModel(t, &now, s)
	m.EnteredAt = map[string]time.Time{"a": seenT0.Add(time.Hour)}
	require.False(t, isUnseen(m, s))
}

func TestSeen_FirstStartAllSeen(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	m = seenBoot(t, m,
		protocol.SessionSummary{ID: "r", Slug: "r", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "d", Slug: "d", State: protocol.StateDone},
		protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking})
	for _, s := range m.Sessions {
		require.False(t, isUnseen(m, s), s.ID)
		require.Equal(t, seenT0, m.SeenAt[s.ID])
	}
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "w", map[string]any{"state": "needs_input"})
	require.True(t, isUnseen(m, m.Sessions[2]))
}

func TestSeen_SavedSeenNoFlood(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	m.SeenAt = map[string]time.Time{"old": seenT0.Add(-time.Hour)}
	m = seenBoot(t, m,
		protocol.SessionSummary{ID: "new", Slug: "n", State: protocol.StateDone},
		protocol.SessionSummary{ID: "old", Slug: "o", State: protocol.StateDone})
	for _, s := range m.Sessions {
		require.False(t, isUnseen(m, s), s.ID)
	}
	require.Equal(t, seenT0, m.SeenAt["new"])
	require.Equal(t, seenT0.Add(-time.Hour), m.SeenAt["old"], "a saved time is kept")
}

func TestSeen_ReconnectNoFlood(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	m = seenBoot(t, m, protocol.SessionSummary{ID: "d", Slug: "d", State: protocol.StateDone})
	now = seenT0.Add(time.Minute)
	m = seenReconnect(m, protocol.SessionSummary{ID: "d", Slug: "d", State: protocol.StateDone})
	require.False(t, isUnseen(m, m.Sessions[0]))
}

func TestSeen_ReconnectWorkingToReadyIsUnseen(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	m = seenBoot(t, m, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	now = seenT0.Add(time.Minute)
	m = seenReconnect(m, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	require.True(t, isUnseen(m, m.Sessions[0]))
}

func TestSeen_SnapshotEventWorkingToReadyIsUnseen(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	m = seenBoot(t, m, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	now = seenT0.Add(time.Minute)
	raw, err := json.Marshal(protocol.Snapshot{Sessions: []protocol.SessionSummary{{ID: "a", Slug: "a", State: protocol.StateDone}}})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: raw})
	require.True(t, isUnseen(m, m.Sessions[0]))
}

func TestSeen_PrunedOnSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", State: protocol.StateDone})
	m.SeenAt = map[string]time.Time{"a": seenT0, "gone": seenT0}
	require.NoError(t, SaveTUIState(m))
	b, err := os.ReadFile(paths.ForHome(home).TUIStateFile)
	require.NoError(t, err)
	var raw struct {
		Seen map[string]time.Time `json:"seen"`
	}
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Len(t, raw.Seen, 1)
	require.Contains(t, raw.Seen, "a")
	// The in-memory map is untouched.
	require.Contains(t, m.SeenAt, "gone")
}

func TestSeen_OffWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", State: protocol.StateWorking})
	m = rowIconsOff(m)
	m = seenBoot(t, m, protocol.SessionSummary{ID: "a", State: protocol.StateWorking})
	markSeen(&m, "a")
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "done"})
	require.Empty(t, m.EnteredAt)
	require.Empty(t, m.SeenAt)
	m.SeenAt = map[string]time.Time{"a": seenT0} // even a stray map is not written
	require.NoError(t, SaveTUIState(m))
	b, err := os.ReadFile(paths.ForHome(home).TUIStateFile)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"seen"`)
}

func TestSeen_RestoredFromState(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now)
	next, _ := m.Update(stateRestoreResultMsg{OK: true, State: persistedState{Seen: map[string]time.Time{"a": seenT0}}})
	require.Equal(t, seenT0, next.(Model).SeenAt["a"])
	off := rowIconsOff(m)
	next, _ = off.Update(stateRestoreResultMsg{OK: true, State: persistedState{Seen: map[string]time.Time{"a": seenT0}}})
	require.Empty(t, next.(Model).SeenAt)
}

func TestSeen_UnseenRendersInStripAndFit(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", Slug: "alpha", State: protocol.StateWorking})
	require.NoError(t, m.Store.Set("column_layout", "fit"))
	markSeen(&m, "a")
	require.Zero(t, boardLayout(m, 120).slots[slotUnseen], "nothing unseen: no slot in fit")
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "done"})
	require.Equal(t, slotWidth[slotUnseen], boardLayout(m, 120).slots[slotUnseen])
	require.Contains(t, boardText(m), "✦")
}

func TestSeen_DetachClears(t *testing.T) {
	now := seenT0
	m := seenModel(t, &now, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	markSeen(&m, "a") // attach at t0
	m.Attach = &AttachState{SessionID: "a"}
	m.Focus = FocusAttach
	now = seenT0.Add(time.Minute)
	m = seenPatch(t, m, "a", map[string]any{"state": "needs_input"})
	require.True(t, isUnseen(m, m.Sessions[0]), "unseen while still attached")
	now = seenT0.Add(2 * time.Minute)
	m = closeAttach(m)
	require.False(t, isUnseen(m, m.Sessions[0]))

	off := rowIconsOff(seenModel(t, &now, protocol.SessionSummary{ID: "a"}))
	off.Attach = &AttachState{SessionID: "a"}
	off = closeAttach(off)
	require.Empty(t, off.SeenAt)
}
