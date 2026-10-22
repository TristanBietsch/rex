package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// LastEventAt is the "end" of a finished session in stats/digest and the
// board's "time ago". Metadata edits must not move it; only real session
// activity (state changes, new output, exit) may.

func doneSession(t *testing.T, s *Store) (*Session, time.Time) {
	t.Helper()
	end := time.Date(2026, 1, 2, 10, 30, 0, 0, time.UTC)
	sess := &Session{
		ID: "full-id", ShortID: "abcd", ToolID: "echo", ModelID: "short",
		Slug: "s", Title: "t", CWD: "/tmp", State: protocol.StateDone,
		StartedAt: end.Add(-time.Hour), LastEventAt: end,
	}
	require.NoError(t, s.Add(sess))
	return sess, end
}

func TestStore_MetadataEditsLeaveLastEventAt(t *testing.T) {
	cases := []struct {
		name string
		edit func(s *Store, id string) error
	}{
		{"rename slug+title", func(s *Store, id string) error { return s.UpdateRename(id, "new-slug", "new title") }},
		{"rename title only (archive)", func(s *Store, id string) error { return s.UpdateRename(id, "", "[archived] t") }},
		{"rename no-op", func(s *Store, id string) error { return s.UpdateRename(id, "", "") }},
		{"set fleet", func(s *Store, id string) error { return s.SetFleet(id, "backend") }},
		{"clear fleet", func(s *Store, id string) error { return s.SetFleet(id, "") }},
		{"description", func(s *Store, id string) error { return s.UpdateDescription(id, "wrapped up the refactor") }},
		{"name", func(s *Store, id string) error { _, err := s.SetName(id, "api"); return err }},
		{"retire", func(s *Store, id string) error {
			_, err := s.SetRetire(id, time.Now().Add(time.Hour), time.Time{})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore()
			sess, end := doneSession(t, s)
			events, cancel := collect(s)
			defer cancel()

			require.NoError(t, tc.edit(s, sess.ID))

			require.True(t, sess.Summary().LastEventAt.Equal(end), "LastEventAt moved: %v", sess.Summary().LastEventAt)
			for _, e := range events() {
				require.NotContains(t, e.Patch, "last_event_at", "metadata patch must not carry last_event_at")
			}
		})
	}
}

func TestStore_ActivityBumpsLastEventAt(t *testing.T) {
	cases := []struct {
		name      string
		act       func(s *Store, sess *Session) error
		wantPatch bool
	}{
		{"transition", func(s *Store, sess *Session) error { return s.Transition(sess.ID, protocol.StateWorking) }, true},
		{"last line", func(s *Store, sess *Session) error { return s.UpdateLastLine(sess.ID, "compiling") }, true},
		{"exit", func(_ *Store, sess *Session) error { sess.SetExit(protocol.StateFailed, nil); return nil }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore()
			sess, end := doneSession(t, s)
			events, cancel := collect(s)
			defer cancel()

			require.NoError(t, tc.act(s, sess))

			got := sess.Summary().LastEventAt
			require.True(t, got.After(end), "LastEventAt not bumped: %v", got)
			if tc.wantPatch {
				evs := events()
				require.NotEmpty(t, evs)
				require.Equal(t, got, evs[len(evs)-1].Patch["last_event_at"], "patch carries the stored timestamp")
			}
		})
	}
}
