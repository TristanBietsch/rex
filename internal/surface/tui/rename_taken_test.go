package tui

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func takenBoard(fn *fakeNamer) Model {
	m := boardModel(
		protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "s-a", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "b", ShortID: "bbbb", Slug: "s-b", State: protocol.StateDone, Name: "Fix Auth"},
	)
	m.namer = fn
	return m
}

func nameTakenEvent(t *testing.T, id, msg string) protocol.Envelope {
	t.Helper()
	data, err := json.Marshal(protocol.ErrorEvent{ID: id, Code: protocol.ErrCodeNameTaken, Message: msg})
	require.NoError(t, err)
	return protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventError, ID: id, Data: data}
}

// A name another session on the board holds keeps the editor open with the
// draft and an inline error; nothing is sent. Typing clears the error.
func TestRename_TakenNameKeepsEditorOpen(t *testing.T) {
	fn := &fakeNamer{}
	m := takenBoard(fn)

	m, _ = updateKey(m, key("r"))
	m = typeText(m, "fix  AUTH")
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Empty(t, fn.calls, "nothing sent")
	require.Equal(t, FocusRename, m.Focus)
	require.NotNil(t, m.Rename)
	require.Equal(t, "fix  AUTH", m.Rename.Edit.Text, "draft kept")
	require.Equal(t, `name "Fix Auth" already in use by bbbb`, m.Rename.Err)
	require.Contains(t, renderRenamePrompt(m, 160), "already in use by bbbb")

	m = typeText(m, "2")
	require.Empty(t, m.Rename.Err, "typing clears the error")
	m, cmd = updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, []string{"a=fix AUTH2"}, fn.calls)
}

// The holder may change the case of its own name.
func TestRename_OwnNameCaseChangeAllowed(t *testing.T) {
	fn := &fakeNamer{}
	m := takenBoard(fn)
	m.SelectedID = "b"
	m, _ = updateKey(m, key("r"))
	m, _ = updateKey(m, key("ctrl+u"))
	m = typeText(m, "fix auth")
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, []string{"b=fix auth"}, fn.calls)
}

// A refusal from the daemon (a race the board could not see) reopens the
// editor with the name that was sent and the daemon's message.
func TestRename_DaemonRefusalReopensEditor(t *testing.T) {
	fn := &fakeNamer{}
	m := takenBoard(fn)
	m, _ = updateKey(m, key("r"))
	m = typeText(m, "Deploy")
	m, cmd := updateKey(m, key("enter"))
	runCmd(cmd)
	require.Equal(t, []string{"a=Deploy"}, fn.calls)
	require.Equal(t, FocusBoard, m.Focus)

	m = m.applyEvent(nameTakenEvent(t, "rename-a", `name "deploy" already in use by cccc`))
	require.Equal(t, FocusRename, m.Focus)
	require.NotNil(t, m.Rename)
	require.Equal(t, "a", m.Rename.SessionID)
	require.Equal(t, "Deploy", m.Rename.Edit.Text)
	require.Equal(t, `name "deploy" already in use by cccc`, m.Rename.Err)
	require.Empty(t, m.Notice, "the optimistic notice is withdrawn")
}

// With another dialog open the refusal goes to the status line instead.
func TestRename_DaemonRefusalWhileBusyGoesToStatus(t *testing.T) {
	fn := &fakeNamer{}
	m := takenBoard(fn)
	m, _ = updateKey(m, key("r"))
	m = typeText(m, "Deploy")
	m, _ = updateKey(m, key("enter"))
	m.Focus = FocusCommand

	m = m.applyEvent(nameTakenEvent(t, "rename-a", `name "deploy" already in use by cccc`))
	require.Equal(t, FocusCommand, m.Focus)
	require.Nil(t, m.Rename)
	require.Contains(t, m.Err, "already in use by cccc")
}

// `:name <text>` checks the board first and refuses locally.
func TestNameCommand_TakenNameRefusedLocally(t *testing.T) {
	fn := &fakeNamer{}
	m := takenBoard(fn)
	m, cmd := runName(m, cmdCall{Verb: "name", Line: "name FIX AUTH", Args: []string{"FIX", "AUTH"}})
	runCmd(cmd)
	require.Empty(t, fn.calls)
	require.Equal(t, `name: name "Fix Auth" already in use by bbbb`, m.Err)

	m, cmd = runName(m, cmdCall{Verb: "name", Line: "name Other", Args: []string{"Other"}})
	runCmd(cmd)
	require.Equal(t, []string{"a=Other"}, fn.calls)
}

func TestNameConflict(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "a", ShortID: "aaaa", Name: "Alpha"},
		{ID: "b", ShortID: "bbbb", Name: "alpha"}, // a legacy duplicate
		{ID: "c", ShortID: "cccc"},
	}
	require.Empty(t, nameConflict(sessions, "c", ""), "empty never conflicts")
	require.Empty(t, nameConflict(sessions, "c", "beta"))
	require.Equal(t, `name "Alpha" already in use by aaaa`, nameConflict(sessions, "c", "ALPHA"))
	require.Empty(t, nameConflict(sessions, "b", "ALPHA"), "own name, even when shared")
	require.Empty(t, nameConflict(sessions, "a", "alpha"))
}
