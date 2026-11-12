package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Reopen announces itself with EventReopened, after the row is live again, and
// only when it succeeds. Subscribers that do not care ignore the kind.
func TestReopen_BroadcastsEventReopened(t *testing.T) {
	s := NewStore()
	exitedSession(t, s, "a", protocol.StateDone)
	var got []Event
	var stateAtEvent protocol.State
	s.Subscribe(func(e Event) {
		got = append(got, e)
		if st, ok := s.CurrentState(e.SessionID); ok {
			stateAtEvent = st
		}
	})
	require.Error(t, s.Reopen("missing"))
	require.Empty(t, got)

	require.NoError(t, s.Reopen("a"))
	require.Len(t, got, 1)
	require.Equal(t, EventReopened, got[0].Kind)
	require.Equal(t, "a", got[0].SessionID)
	require.Equal(t, protocol.StateQueued, stateAtEvent, "the event follows the change")

	addSession(t, s, "live", protocol.StateWorking)
	got = nil
	require.Error(t, s.Reopen("live"))
	require.Empty(t, got, "a refused reopen broadcasts nothing")
}
