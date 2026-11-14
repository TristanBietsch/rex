package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUndone_UnmarkedDoesNotMark(t *testing.T) {
	m := commandModel(t)
	fr := m.retirer.(*fakeRetirer)
	next, cmd := executeCommand(m, "undone alpha")
	runCmd(cmd)
	require.Empty(t, fr.retired, ":undone must never mark a session done")
	require.Empty(t, fr.unretired)
	require.Equal(t, "alpha is not marked done", next.Notice)
}

func TestUndone_MarkedUnmarks(t *testing.T) {
	m := commandModel(t)
	fr := m.retirer.(*fakeRetirer)
	m.Sessions[0].RetireAt = time.Now().Add(time.Hour)
	next, cmd := executeCommand(m, "undone alpha")
	runCmd(cmd)
	require.Equal(t, []string{"id-alpha"}, fr.unretired)
	require.Empty(t, fr.retired)
	require.Contains(t, next.Notice, "unmarked")
}

func TestDone_StillToggles(t *testing.T) {
	m := commandModel(t)
	fr := m.retirer.(*fakeRetirer)
	_, cmd := executeCommand(m, "done alpha")
	runCmd(cmd)
	require.Equal(t, []string{"id-alpha"}, fr.retired)
}

func attachCommandModel(t *testing.T) Model {
	m := commandModel(t)
	m.Attach = &AttachState{SessionID: "id-alpha"}
	return m
}

func TestAttachUndone_UnmarkedDoesNotMark(t *testing.T) {
	m := attachCommandModel(t)
	fr := m.retirer.(*fakeRetirer)
	next, cmd := executeAttachCommand(m, "undone")
	runCmd(cmd)
	require.Empty(t, fr.retired, "::undone must never mark a session done")
	require.Empty(t, fr.unretired)
	require.Equal(t, "alpha is not marked done", next.Notice)
}

func TestAttachUndone_MarkedUnmarks(t *testing.T) {
	m := attachCommandModel(t)
	fr := m.retirer.(*fakeRetirer)
	m.Sessions[0].RetireAt = time.Now().Add(time.Hour)
	next, cmd := executeAttachCommand(m, "undone")
	runCmd(cmd)
	require.Equal(t, []string{"id-alpha"}, fr.unretired)
	require.Empty(t, fr.retired)
	require.Contains(t, next.Notice, "unmarked")
}

func TestAttachDone_StillToggles(t *testing.T) {
	m := attachCommandModel(t)
	fr := m.retirer.(*fakeRetirer)
	_, cmd := executeAttachCommand(m, "done")
	runCmd(cmd)
	require.Equal(t, []string{"id-alpha"}, fr.retired)
}
