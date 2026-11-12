package server

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Contract with the summarizer: a resume announces the new run with the
// store's EventReopened (the summarizer restarts its schedule and re-arms the
// final summary from it), and a resume that is refused does not.
func TestResume_AnnouncesReopenedToStoreSubscribers(t *testing.T) {
	e := startResumeServer(t, resumeTools, 0)
	sess := exited(func() string { return e.dir }, "a", protocol.StateDone, "slow", testResumeID)
	require.NoError(t, e.store.Add(sess))
	require.NoError(t, state.WriteMeta(e.dir, sess))
	var mu sync.Mutex
	var kinds []state.EventKind
	e.store.Subscribe(func(ev state.Event) {
		mu.Lock()
		kinds = append(kinds, ev.Kind)
		mu.Unlock()
	})
	conn, w, r := e.dial(t)

	code, msg := call(t, conn, w, r, protocol.IntentResume, protocol.Resume{SessionID: "a"})
	require.Empty(t, code, msg)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, k := range kinds {
			if k == state.EventReopened {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, e.srv.deleteSession("a", protocol.RemovedByUser))
}
