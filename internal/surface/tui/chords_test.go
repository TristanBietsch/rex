package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func chordModel() (Model, *fakeRetirer) {
	fr := &fakeRetirer{}
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateWorking},
	)
	m.retirer = fr
	return m, fr
}

func TestBoardChords(t *testing.T) {
	t.Run("dd opens delete confirm", func(t *testing.T) {
		m, _ := chordModel()
		m, _ = updateKey(m, key("d"))
		require.Equal(t, "d", m.PendingChord)
		m, _ = updateKey(m, key("d"))
		require.Equal(t, FocusConfirmDelete, m.Focus)
		require.Equal(t, "a", m.PendingDeleteID)
		require.Empty(t, m.PendingChord)
	})
	t.Run("cc retires", func(t *testing.T) {
		m, fr := chordModel()
		m, _ = updateKey(m, key("c"))
		_, cmd := updateKey(m, key("c"))
		runCmd(cmd)
		require.Equal(t, []string{"a"}, fr.retired)
	})
	t.Run("c then d arms d", func(t *testing.T) {
		m, fr := chordModel()
		m, _ = updateKey(m, key("c"))
		m, _ = updateKey(m, key("d"))
		require.Equal(t, "d", m.PendingChord)
		require.Equal(t, FocusBoard, m.Focus)
		require.Empty(t, fr.retired)
	})
	t.Run("d then j moves and clears the chord", func(t *testing.T) {
		m, _ := chordModel()
		m, _ = updateKey(m, key("d"))
		m, _ = updateKey(m, key("j"))
		require.Empty(t, m.PendingChord)
		require.Equal(t, "b", m.SelectedID)
		require.Equal(t, FocusBoard, m.Focus)
	})
	t.Run("chord keys keep the notice, others clear it", func(t *testing.T) {
		m, _ := chordModel()
		m.Notice = "hello"
		m, _ = updateKey(m, key("d"))
		require.Equal(t, "hello", m.Notice)
		m, _ = updateKey(m, key("j"))
		require.Empty(t, m.Notice)
	})
	t.Run("chord with no selection is a no-op", func(t *testing.T) {
		m, fr := chordModel()
		m.SelectedID = ""
		m, _ = updateKey(m, key("c"))
		m, cmd := updateKey(m, key("c"))
		require.Nil(t, cmd)
		require.Empty(t, fr.retired)
		require.Empty(t, m.PendingChord)
	})
}

func TestChordHandlerReportsUnhandled(t *testing.T) {
	m, _ := chordModel()
	_, _, ok := boardChords["d"](m, "x")
	require.False(t, ok, "second key that doesn't complete the chord is not consumed")
}
