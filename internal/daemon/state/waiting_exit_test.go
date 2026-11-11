package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The exit paths call SetExit before Transition; the terminal patch still
// carries the clear.
func TestStore_TerminalTransitionAfterSetExitPatchesWaitCleared(t *testing.T) {
	s, patches, mu := waitingStore(t, protocol.StateWorking)
	require.NoError(t, s.SetWaiting("a", protocol.WaitShell, time.Now().UTC()))
	sess, _ := s.Get("a")
	sess.SetExit(protocol.StateDone, nil)
	require.NoError(t, s.Transition("a", protocol.StateDone))
	mu.Lock()
	defer mu.Unlock()
	last := (*patches)[len(*patches)-1]
	require.Contains(t, last, "waiting_on")
	require.Nil(t, last["waiting_on"])
}
