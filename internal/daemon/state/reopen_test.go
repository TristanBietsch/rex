package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func exitedSession(t *testing.T, s *Store, id string, st protocol.State) *Session {
	t.Helper()
	code := 3
	sess := addSession(t, s, id, st)
	sess.ExitCode = &code
	sess.EndedAt = time.Now().UTC().Add(-time.Hour)
	sess.RetireAt = time.Now().UTC().Add(time.Hour)
	sess.RetiredAt = time.Now().UTC()
	sess.Description = "kept"
	sess.Tokens = 42
	sess.ResumeID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	return sess
}

func TestReopen_ClearsEndAndKeepsIdentity(t *testing.T) {
	for _, st := range []protocol.State{protocol.StateDone, protocol.StateFailed, protocol.StateCrashed} {
		t.Run(string(st), func(t *testing.T) {
			s := NewStore()
			sess := exitedSession(t, s, "a", st)
			started := sess.StartedAt
			before := time.Now()
			require.NoError(t, s.Reopen("a"))

			require.Equal(t, protocol.StateQueued, sess.State)
			require.False(t, sess.LastEventAt.Before(before.UTC().Add(-time.Second)))
			require.True(t, sess.EndedAt.IsZero())
			require.Nil(t, sess.ExitCode)
			require.True(t, sess.RetireAt.IsZero())
			require.True(t, sess.RetiredAt.IsZero())
			require.Equal(t, started, sess.StartedAt)
			require.Equal(t, "kept", sess.Description)
			require.EqualValues(t, 42, sess.Tokens)
			require.Equal(t, "a", sess.Slug)
		})
	}
}

func TestReopen_Refusals(t *testing.T) {
	s := NewStore()
	for _, st := range []protocol.State{protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput} {
		addSession(t, s, string(st), st)
		require.ErrorIs(t, s.Reopen(string(st)), ErrNotExited, st)
	}

	reaped := exitedSession(t, s, "reaped", protocol.StateDone)
	reaped.RetireAt = time.Now().Add(-time.Second)
	claimed, _ := s.ClaimExpired(time.Now())
	require.Len(t, claimed, 1)
	require.ErrorIs(t, s.Reopen("reaped"), ErrReaping)
	require.Equal(t, protocol.StateDone, reaped.State, "refusal changes nothing")

	require.Error(t, s.Reopen("missing"))

	exitedSession(t, s, "gone", protocol.StateDone)
	require.NoError(t, s.RemoveWithReason("gone", protocol.RemovedByUser))
	require.Error(t, s.Reopen("gone"))
}

func TestResetHookFile_TruncatesAndKeepsMode(t *testing.T) {
	root := t.TempDir()
	path, err := CreateHookFile(root, "a")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("needs_input\nsid x\n"), 0o600))

	got, err := ResetHookFile(root, "a")
	require.NoError(t, err)
	require.Equal(t, path, got)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Empty(t, raw)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm()&0o600)
}

func TestResetHookFile_NeverCreatesSessionDir(t *testing.T) {
	root := t.TempDir()
	_, err := ResetHookFile(root, "gone")
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(root, "sessions", "gone"))
	require.True(t, os.IsNotExist(statErr), "a deleted session's dir must not come back")

	_, err = ResetHookFile(root, "../x")
	require.ErrorIs(t, err, ErrUnsafeSessionID)
}

// SetExit clears WorkingSince before Transition sees it, so Transition must
// still tell clients: a terminal change always carries working_since: nil
// (docs/protocol.md), also on the path where the supervisor calls SetExit
// first.
func TestTransition_TerminalAfterSetExitStillPatchesWorkingSinceNull(t *testing.T) {
	s := NewStore()
	sess := &Session{ID: "w", ShortID: "w", State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, s.Add(sess))
	require.NoError(t, s.Transition("w", protocol.StateWorking))
	require.False(t, sess.WorkingSince.IsZero())
	patch := lastPatch(t, s)

	sess.SetExit(protocol.StateDone, nil)
	require.NoError(t, s.Transition("w", protocol.StateDone))
	v, ok := patch()["working_since"]
	require.True(t, ok, "patch %v", patch())
	require.Nil(t, v)
	require.True(t, sess.WorkingSince.IsZero())
}
