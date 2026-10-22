package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func attachedModel() Model {
	m := boardModel(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateNeedsInput})
	m.Attach = &AttachState{SessionID: "a"}
	m.Focus = FocusAttach
	return m
}

func TestAttach_DoubleColonOpensCommandBar(t *testing.T) {
	m := attachedModel()
	m, cmd := updateKey(m, key(":"))
	require.NotNil(t, cmd, "first ':' is held with a flush timer")
	require.True(t, m.Attach.colonPending)
	require.Equal(t, FocusAttach, m.Focus)

	m, _ = updateKey(m, key(":"))
	require.Equal(t, FocusCommand, m.Focus)
	require.False(t, m.Attach.colonPending)

	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusAttach, m.Focus)
	require.NotNil(t, m.Attach)
}

func TestAttach_CommandBarQuitDetaches(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "q")
	m, _ = updateKey(m, key("enter"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestAttach_CommandBarDoneMarksAttachedSession(t *testing.T) {
	fr := &fakeRetirer{}
	m := attachedModel()
	m.retirer = fr
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "done")
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, []string{"a"}, fr.retired)
	require.Equal(t, FocusAttach, m.Focus, "stays attached")
}

func TestAttach_CommandBarUnknownVerbFallsBackToBoard(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	m, _ = updateKey(m, key(":"))
	m = typeText(m, "stats")
	m, _ = updateKey(m, key("enter"))
	require.Nil(t, m.Attach)
	require.Equal(t, FocusStats, m.Focus)
}

func TestAttach_ColonFlushIgnoresStaleOrEnded(t *testing.T) {
	m := attachedModel()
	m, _ = updateKey(m, key(":"))
	seq := m.Attach.colonSeq

	m, _ = flushAttachColon(m, attachColonTimeoutMsg{SessionID: "a", Seq: seq - 1})
	require.True(t, m.Attach.colonPending, "stale timer must not flush")

	m.Attach.Ended = true // nothing can be sent to an ended session
	m, _ = flushAttachColon(m, attachColonTimeoutMsg{SessionID: "a", Seq: seq})
	require.False(t, m.Attach.colonPending)
}

func TestAttach_PastedColonsPassThrough(t *testing.T) {
	require.False(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("::"), Paste: true}))
	require.False(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":"), Paste: true}))
	require.True(t, isLoneColon(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}))
}
