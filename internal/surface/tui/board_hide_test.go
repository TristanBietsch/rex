package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func plainBoard(m Model) []string {
	return strings.Split(ansi.Strip(renderBoard(m, 120, 30)), "\n")
}

func TestHideSectionRemovesIt(t *testing.T) {
	m := filterModel()
	m = setSectionHidden(m, "done", true)
	require.Empty(t, m.Err)
	require.True(t, m.Hidden["done"])

	var ids []string
	for _, s := range orderedSessions(m) {
		ids = append(ids, s.ID)
	}
	require.NotContains(t, ids, "d1")
	require.Contains(t, strings.Join(plainBoard(m), "\n"), "NEEDS YOU")
	require.NotContains(t, strings.Join(plainBoard(m), "\n"), "DONE")

	// Hiding the first section leaves no leading gap.
	m = setSectionHidden(m, "done", false)
	m = setSectionHidden(m, "needs", true)
	m = setSectionHidden(m, "waiting", true)
	lines := plainBoard(m)
	require.Contains(t, lines[0], "WORKING", "first line is the first shown title, not a gap")
	require.Equal(t, 1, strings.Count(strings.Join(lines, "\n"), "WORKING"))
}

func TestSetSectionHiddenIsCopyOnWrite(t *testing.T) {
	m := filterModel()
	m = setSectionHidden(m, "done", true)
	before := m
	after := setSectionHidden(m, "working", true)
	require.True(t, after.Hidden["working"])
	require.False(t, before.Hidden["working"], "the earlier model's map is untouched")
}

func TestCannotHideEverySection(t *testing.T) {
	m := filterModel()
	m = setSectionHidden(m, "needs", true)
	m = setSectionHidden(m, "waiting", true)
	m = setSectionHidden(m, "working", true)
	m = setSectionHidden(m, "done", true)
	require.Equal(t, "filter: can't hide every section", m.Err)
	require.False(t, m.Hidden["done"])
}

func TestSelectionLeavesHiddenSection(t *testing.T) {
	t.Run("toggle", func(t *testing.T) {
		m := filterModel()
		m.SelectedID = "d1"
		m = setSectionHidden(m, "done", true)
		require.Equal(t, "n1", m.SelectedID)
	})
	t.Run("live patch", func(t *testing.T) {
		m := filterModel()
		m = setSectionHidden(m, "done", true)
		m.SelectedID = "w1"
		raw, err := json.Marshal(protocol.SessionUpdated{SessionID: "w1", Patch: map[string]any{
			"state": string(protocol.StateDone),
		}})
		require.NoError(t, err)
		next, _ := m.Update(DaemonEventMsg{Env: protocol.Envelope{Type: protocol.EventSessionUpdated, Data: raw}})
		got := next.(Model)
		require.Equal(t, "n1", got.SelectedID, "w1 finished into the hidden Done section")
	})
}

func TestJumpToHiddenSectionNotice(t *testing.T) {
	m := filterModel()
	m = setSectionHidden(m, "needs", true)
	m = setSectionHidden(m, "done", true)
	m.SelectedID = "w1"

	for _, tc := range []struct{ key, notice string }{
		{"1", "Needs you is hidden — z1 to show"},
		{"4", "Done is hidden — zh to show"},
		{"tab", "Needs you is hidden — z1 to show"},
	} {
		got, _ := updateKey(m, key(tc.key))
		require.Equal(t, tc.notice, got.Notice, tc.key)
		require.Equal(t, "w1", got.SelectedID, tc.key)
	}
}

func TestSpawnedSessionHiddenByFilterNotice(t *testing.T) {
	m := filterModel()
	m = setSectionHidden(m, "needs", true)
	m.PendingSelectSlug = "fresh"
	raw, err := json.Marshal(protocol.SessionSummary{
		ID: "f1", Slug: "fresh", ToolID: "claude", State: protocol.StateNeedsInput})
	require.NoError(t, err)
	next, _ := m.Update(DaemonEventMsg{Env: protocol.Envelope{Type: protocol.EventSessionAdded, Data: raw}})
	got := next.(Model)
	require.Equal(t, "fresh is hidden by the filter", got.Notice)
}
