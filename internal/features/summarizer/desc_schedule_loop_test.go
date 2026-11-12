package summarizer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Loop wiring, real timers. Only lower bounds and order are asserted: a slow
// machine makes calls late, never early.
func TestSchedule_LoopFiresTicksOnTheGridForASessionAddedLater(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 60 * time.Millisecond, EarlyCount: 2, LateEvery: 120 * time.Millisecond}
	st := state.NewStore()
	w := startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil)) // idle loop, no timer armed
	addLive(t, st, "sess-loop", time.Now())

	require.Eventually(t, func() bool { return f.count() >= 4 }, 10*time.Second, 5*time.Millisecond)
	w.mu.Lock()
	anchor := w.sched["sess-loop"].anchor
	w.mu.Unlock()
	for i := range 4 {
		due := c.Due(anchor, i+1)
		require.False(t, f.time(i).Before(due), "call %d came before tick %d", i, i+1)
	}
	require.False(t, progressSys(f.system(0)) || progressSys(f.system(1)))
	require.True(t, progressSys(f.system(2)) && progressSys(f.system(3)))
}

// New session: the first state change runs before any output (empty screen,
// nothing to describe). The first output must still produce a description
// without waiting for tick 1; after it, output sends are dropped.
func TestSchedule_FirstDescriptionDoesNotWaitForTickOne(t *testing.T) {
	f := newRecOllama(t)
	var mu sync.Mutex
	screen := ""
	tr := func(string, int) []byte {
		mu.Lock()
		defer mu.Unlock()
		return []byte(screen)
	}
	st := state.NewStore()
	addLive(t, st, "sess-new", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, DefaultCadence, true), st, tr)

	require.NoError(t, st.Transition("sess-new", protocol.StateWorking)) // empty screen: skipped
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, f.count())

	mu.Lock()
	screen = "running pnpm test"
	mu.Unlock()
	w.Channel() <- "sess-new"
	require.Eventually(t, func() bool {
		sess, _ := st.Get("sess-new")
		return sess.Summary().Description != ""
	}, 10*time.Second, 5*time.Millisecond)
	require.Equal(t, 1, f.count())

	mu.Lock()
	screen = "running pnpm test, more output"
	mu.Unlock()
	w.Channel() <- "sess-new"
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 1, f.count(), "with a description, output sends are dropped")
}
