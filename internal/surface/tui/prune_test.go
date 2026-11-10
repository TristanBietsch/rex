package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type fakeDeleter struct {
	deleted []string
	fail    map[string]bool
}

func (f *fakeDeleter) Delete(id string) error {
	if f.fail[id] {
		return errors.New("socket closed")
	}
	f.deleted = append(f.deleted, id)
	return nil
}

// pruneFixture has one session per board situation. Marked sessions sit in
// Completed whatever their live state.
func pruneFixture() []protocol.SessionSummary {
	marked := time.Now().Add(time.Hour)
	return []protocol.SessionSummary{
		{ID: "needs", Slug: "needs", State: protocol.StateNeedsInput},
		{ID: "working", Slug: "working", State: protocol.StateWorking},
		{ID: "queued", Slug: "queued", State: protocol.StateQueued},
		{ID: "done", Slug: "done", State: protocol.StateDone},
		{ID: "failed", Slug: "failed", State: protocol.StateFailed},
		{ID: "crashed", Slug: "crashed", State: protocol.StateCrashed},
		{ID: "marked-idle", Slug: "marked-idle", State: protocol.StateNeedsInput, RetireAt: marked},
		{ID: "marked-busy", Slug: "marked-busy", State: protocol.StateWorking, RetireAt: marked},
		{ID: "marked-queued", Slug: "marked-queued", State: protocol.StateQueued, RetireAt: marked},
	}
}

func ids(ss []protocol.SessionSummary) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}

func TestPruneTargets(t *testing.T) {
	targets, kept := PruneTargets(pruneFixture())
	require.Equal(t, []string{"done", "failed", "crashed", "marked-idle"}, ids(targets))
	require.Equal(t, 2, kept, "marked-busy and marked-queued are still working")

	// Every target is on the Completed column; nothing from other columns.
	for _, s := range targets {
		require.True(t, inCompleted(s), s.ID)
	}
}

func TestPruneCommandConfirmsThenDeletes(t *testing.T) {
	fd := &fakeDeleter{}
	m := boardModel(pruneFixture()...)
	m.deleter = fd

	m = typeCommand(m, "prune")
	require.Equal(t, FocusConfirmDelete, m.Focus)
	require.Equal(t, []string{"done", "failed", "crashed", "marked-idle"}, m.PendingPrune)
	require.Empty(t, fd.deleted, "nothing is deleted before y")
	require.Contains(t, renderDeleteConfirm(m, 200), "clear 4 completed?")
	require.Contains(t, renderDeleteConfirm(m, 200), "(2 still working kept)")

	m, cmd := updateKey(m, key("y"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.PendingPrune)
	require.NotNil(t, cmd)
	msg := cmd()
	require.Equal(t, []string{"done", "failed", "crashed", "marked-idle"}, fd.deleted)

	next, _ := m.Update(msg)
	m = next.(Model)
	require.Equal(t, "cleared 4 (2 still working kept)", m.Notice)
}

func TestPruneCommandCancel(t *testing.T) {
	for _, k := range []string{"n", "esc"} {
		fd := &fakeDeleter{}
		m := boardModel(pruneFixture()...)
		m.deleter = fd
		m = typeCommand(m, "prune")
		m, cmd := updateKey(m, key(k))
		runCmd(cmd)
		require.Equal(t, FocusBoard, m.Focus, k)
		require.Nil(t, m.PendingPrune, k)
		require.Empty(t, fd.deleted, k)
	}
}

func TestPruneCommandNothingToClear(t *testing.T) {
	fd := &fakeDeleter{}
	m := boardModel(
		protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateWorking, RetireAt: time.Now().Add(time.Hour)},
	)
	m.deleter = fd
	m = typeCommand(m, "prune")
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, "nothing to clear (1 still working kept)", m.Notice)
	require.Empty(t, fd.deleted)
}

func TestPruneReportsFailures(t *testing.T) {
	fd := &fakeDeleter{fail: map[string]bool{"failed": true}}
	m := boardModel(pruneFixture()...)
	m.deleter = fd
	m = typeCommand(m, "prune")
	m, cmd := updateKey(m, key("y"))
	next, _ := m.Update(cmd())
	m = next.(Model)
	require.Contains(t, m.Err, "prune: 1 of 4 failed")
}
