package summarizer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The tests in this file never start the loop and never sleep: they build a
// Worker, give it a fake clock, feed it store events and call fireDue,
// nextTick and handle directly.

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func (c *fakeClock) advance(d time.Duration) { c.set(c.now().Add(d)) }

var t0 = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// schedRig is a Worker wired to a fake clock and a store, loop not running.
type schedRig struct {
	w   *Worker
	clk *fakeClock
	st  *state.Store
	f   *recOllama
}

func newRig(t *testing.T, c Cadence, on bool) *schedRig {
	t.Helper()
	f := newRecOllama(t)
	st := state.NewStore()
	cfg := schedCfg(f.srv.URL, c, on)
	w := New(cfg, st, uniqueTranscript(nil, nil))
	clk := &fakeClock{t: t0}
	w.now = clk.now
	st.Subscribe(w.onEvent)
	w.seedSchedule()
	return &schedRig{w: w, clk: clk, st: st, f: f}
}

func (r *schedRig) add(t *testing.T, id string) {
	t.Helper()
	addLive(t, r.st, id, r.clk.now())
}

// anchor is the session's effective (jittered) anchor.
func (r *schedRig) anchor(id string) time.Time {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()
	return r.w.sched[id].anchor
}

// take removes and returns the pending request of id.
func (r *schedRig) take(id string) (pendingReq, bool) {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()
	p, ok := r.w.pending[id]
	delete(r.w.pending, id)
	return p, ok
}

func (r *schedRig) handle(id string, req pendingReq) {
	r.w.handle(context.Background(), id, req)
}

func TestSchedDet_TicksInOrderOnTheAnchorGrid(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-grid")
	anchor := r.anchor("sess-grid")
	for k := 1; k <= 8; k++ {
		due := DefaultCadence.Due(anchor, k)
		r.clk.set(due.Add(-time.Nanosecond))
		r.w.fireDue(r.clk.now())
		_, ok := r.take("sess-grid")
		require.False(t, ok, "tick %d is not due yet", k)
		d, armed := r.w.nextTick(r.clk.now())
		require.True(t, armed)
		require.Equal(t, time.Nanosecond, d, "tick %d: timer re-armed for the next due time", k)

		r.clk.set(due)
		r.w.fireDue(r.clk.now())
		p, ok := r.take("sess-grid")
		require.True(t, ok, "tick %d is due", k)
		require.False(t, p.prio)
		require.Equal(t, k > 5, p.late, "tick %d late phase", k)
		require.Equal(t, k, r.w.firedOf("sess-grid"))
	}
	// 5, 10, 15, 20, 25, then every 30, from the (jittered) anchor.
	require.Equal(t, 25*time.Minute, DefaultCadence.Due(anchor, 5).Sub(anchor))
	require.Equal(t, 30*time.Minute, DefaultCadence.Due(anchor, 6).Sub(DefaultCadence.Due(anchor, 5)))
}

func TestSchedDet_StartedAtAnchorsTheGrid(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	started := t0.Add(-12 * time.Minute) // 2 ticks already past
	addLive(t, r.st, "sess-old", started)
	r.clk.set(t0)
	a := r.anchor("sess-old")
	require.LessOrEqual(t, a.Sub(started).Abs(), DefaultCadence.maxJitter())
	require.Equal(t, 2, r.w.firedOf("sess-old"))
	require.Less(t, DefaultCadence.Due(a, 3).Sub(t0), 3*time.Minute+time.Minute) // the third tick is next, on its own grid
	r.clk.set(DefaultCadence.Due(a, 3))
	r.w.fireDue(r.clk.now())
	_, ok := r.take("sess-old")
	require.True(t, ok)
}

func TestSchedDet_SeededSessionCountsPastTicksAsFired(t *testing.T) {
	f := newRecOllama(t)
	st := state.NewStore()
	addLive(t, st, "sess-seed", t0.Add(-12*time.Minute))
	w := New(schedCfg(f.srv.URL, DefaultCadence, true), st, uniqueTranscript(nil, nil))
	clk := &fakeClock{t: t0}
	w.now = clk.now
	w.seedSchedule()
	require.GreaterOrEqual(t, w.firedOf("sess-seed"), 1)
	w.fireDue(t0)
	require.Zero(t, w.pendingLen(), "no catch-up burst at start")
}

func TestSchedDet_CatchUpIsOneStep(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-jump")
	r.clk.advance(10 * time.Hour) // a stall, or a clock jump forward
	r.w.fireDue(r.clk.now())
	p, ok := r.take("sess-jump")
	require.True(t, ok)
	require.True(t, p.late)
	require.Equal(t, DefaultCadence.Elapsed(r.anchor("sess-jump"), r.clk.now()), r.w.firedOf("sess-jump"))
	r.w.fireDue(r.clk.now())
	_, again := r.take("sess-jump")
	require.False(t, again, "one request for all the missed ticks")
}

func TestSchedDet_StateChangeIsImmediateAndDoesNotShiftTicks(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-sc")
	anchor := r.anchor("sess-sc")
	r.clk.advance(time.Minute)
	require.NoError(t, r.st.Transition("sess-sc", protocol.StateNeedsInput))
	p, ok := r.take("sess-sc")
	require.True(t, ok, "state change queues at once")
	require.True(t, p.prio)
	require.False(t, p.late)
	require.Equal(t, anchor, r.anchor("sess-sc"))
	require.Zero(t, r.w.firedOf("sess-sc"))

	r.clk.set(DefaultCadence.Due(anchor, 1))
	r.w.fireDue(r.clk.now())
	_, ok = r.take("sess-sc")
	require.True(t, ok, "tick 1 still lands at anchor+5m")
}

func TestSchedDet_TickMergesWithPendingStateChange(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-mg")
	anchor := r.anchor("sess-mg")
	r.clk.set(DefaultCadence.Due(anchor, 6))
	require.NoError(t, r.st.Transition("sess-mg", protocol.StateNeedsInput))
	r.w.fireDue(r.clk.now())
	require.Equal(t, 1, r.w.pendingLen(), "one pending request")
	p, _ := r.take("sess-mg")
	require.True(t, p.prio && p.late)
}

func TestSchedDet_OutputSends(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-out")
	// No description yet: output is honored so the first description does not wait for a tick.
	r.w.fromOutput("sess-out")
	_, ok := r.take("sess-out")
	require.True(t, ok)

	require.NoError(t, r.st.UpdateDescription("sess-out", "Running tests"))
	r.w.fromOutput("sess-out")
	_, ok = r.take("sess-out")
	require.False(t, ok, "with a description, output is dropped while the schedule is on")

	r.w.SetSchedule(false)
	r.w.fromOutput("sess-out")
	_, ok = r.take("sess-out")
	require.True(t, ok, "honored while off")
}

func TestSchedDet_UnchangedScreenMakesNoCallButTickCounts(t *testing.T) {
	r := newRig(t, Cadence{EarlyEvery: time.Minute, EarlyCount: 2, LateEvery: 2 * time.Minute}, true)
	r.w.transcript = transcriptStub("the very same screen")
	r.add(t, "sess-same")
	anchor := r.anchor("sess-same")
	var late []bool
	for k := 1; k <= 4; k++ {
		r.clk.set(r.w.cadence.Due(anchor, k))
		r.w.fireDue(r.clk.now())
		p, ok := r.take("sess-same")
		require.True(t, ok)
		late = append(late, p.late)
		r.handle("sess-same", p)
	}
	require.Equal(t, []bool{false, false, true, true}, late)
	// tick 1 calls; 2 is unchanged (no call, counted); 3 is a new (progress)
	// prompt, so a call; 4 is unchanged again.
	require.Equal(t, 2, r.f.count())
	require.False(t, progressSys(r.f.system(0)))
	require.True(t, progressSys(r.f.system(1)))
	require.Equal(t, 4, r.w.firedOf("sess-same"))
}

func TestSchedDet_LatePhaseDetailUpAndProgressPrompt(t *testing.T) {
	for _, tc := range []struct {
		set        Detail
		early, lat string
	}{
		{DetailShort, "at most 60 characters", "at most 110 characters"},
		{DetailMedium, "at most 110 characters", "at most 180 characters"},
		{DetailLong, "at most 180 characters", "at most 180 characters"},
	} {
		t.Run(tc.set.String(), func(t *testing.T) {
			r := newRig(t, DefaultCadence, true)
			r.w.SetDetail(tc.set)
			r.add(t, "sess-det")
			r.handle("sess-det", pendingReq{since: t0})
			r.handle("sess-det", pendingReq{since: t0, late: true})
			require.Equal(t, 2, r.f.count())
			require.False(t, progressSys(r.f.system(0)))
			require.Contains(t, r.f.system(0), tc.early)
			require.True(t, progressSys(r.f.system(1)))
			require.Contains(t, r.f.system(1), tc.lat)
		})
	}
}

// A state change, even merged with a late tick, uses the activity prompt at
// the user's detail; so does the final summary of a finished session.
func TestSchedDet_LateTickMergedWithStateChangeOrDoneUsesActivityPrompt(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.w.SetDetail(DetailShort)
	r.add(t, "sess-pr")
	r.handle("sess-pr", pendingReq{since: t0, prio: true, late: true})
	require.False(t, progressSys(r.f.system(0)))
	require.Contains(t, r.f.system(0), "at most 60 characters")

	require.NoError(t, r.st.Transition("sess-pr", protocol.StateDone))
	r.take("sess-pr")
	r.handle("sess-pr", pendingReq{since: t0, late: true})
	require.Equal(t, 2, r.f.count())
	require.False(t, progressSys(r.f.system(1)), "a done session gets the final-summary prompt")
	require.Contains(t, r.f.system(1), "at most 60 characters")
}

func TestSchedDet_RemovedSessionGetsNoMoreTicks(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-rm")
	require.NoError(t, r.st.Remove("sess-rm"))
	require.Zero(t, r.w.schedLen())
	r.clk.advance(10 * time.Hour)
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen())
	_, armed := r.w.nextTick(r.clk.now())
	require.False(t, armed)
}

func TestSchedDet_TerminalSessionGetsNoTicks(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-fin")
	require.NoError(t, r.st.Transition("sess-fin", protocol.StateDone))
	require.Zero(t, r.w.schedLen())
	r.take("sess-fin") // the final summary request
	r.clk.advance(10 * time.Hour)
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen())
}

func TestSchedDet_BackendDownAtMostOnePending(t *testing.T) {
	r := newRig(t, Cadence{EarlyEvery: time.Minute, EarlyCount: 3, LateEvery: time.Hour}, true)
	r.add(t, "sess-down")
	r.w.MarkUnavailable("test")
	anchor := r.anchor("sess-down")
	for k := 1; k <= 3; k++ {
		r.clk.set(r.w.cadence.Due(anchor, k))
		r.w.fireDue(r.clk.now())
	}
	require.Equal(t, 1, r.w.pendingLen())
	_, _, _, ok := r.w.next(time.Now())
	require.False(t, ok, "nothing runs while down")
	r.w.MarkAvailable()
	id, _, _, ok := r.w.next(time.Now())
	require.True(t, ok)
	require.Equal(t, "sess-down", id)
	require.Zero(t, r.w.pendingLen(), "the merged ticks make one request")
}

// Resume: Store.Reopen restarts the early phase and re-arms the final summary.
func TestSchedDet_ReopenRestartsEarlyPhaseAndRearmsFinal(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-reo")
	r.clk.advance(2 * time.Hour)
	require.NoError(t, r.st.Transition("sess-reo", protocol.StateDone))
	r.take("sess-reo")
	r.handle("sess-reo", pendingReq{since: t0, prio: true})
	require.Equal(t, 1, r.f.count(), "final summary")
	r.handle("sess-reo", pendingReq{since: t0, prio: true})
	require.Equal(t, 1, r.f.count(), "only one final per run")
	require.Zero(t, r.w.schedLen())

	require.NoError(t, r.st.Reopen("sess-reo")) // no patch, no NewState: just the store's own signal
	require.Equal(t, 1, r.w.schedLen())
	anchor := r.anchor("sess-reo")
	require.LessOrEqual(t, anchor.Sub(r.clk.now()).Abs(), DefaultCadence.maxJitter(), "anchored at the reopen, not StartedAt")
	require.Zero(t, r.w.firedOf("sess-reo"))
	r.clk.set(DefaultCadence.Due(anchor, 1))
	r.w.fireDue(r.clk.now())
	p, ok := r.take("sess-reo")
	require.True(t, ok)
	require.False(t, p.late, "early phase again")

	// The final summary of the new run is not blocked by the old one.
	require.NoError(t, r.st.Transition("sess-reo", protocol.StateFailed))
	r.take("sess-reo")
	r.handle("sess-reo", pendingReq{since: t0, prio: true})
	require.Equal(t, 2, r.f.count(), "final summary after reopen")
}

func TestSchedDet_FlagFlipOnOffOnWithoutReplay(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-flip")
	anchor := r.anchor("sess-flip")
	r.clk.set(DefaultCadence.Due(anchor, 1))
	r.w.fireDue(r.clk.now())
	r.take("sess-flip")

	r.w.SetSchedule(false)
	r.clk.set(DefaultCadence.Due(anchor, 3).Add(time.Minute)) // ticks 2 and 3 pass while off
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen(), "off: no ticks")
	_, armed := r.w.nextTick(r.clk.now())
	require.False(t, armed)

	r.w.SetSchedule(true)
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen(), "no catch-up calls on the flip")
	require.Equal(t, 3, r.w.firedOf("sess-flip"))
	r.clk.set(DefaultCadence.Due(anchor, 4))
	r.w.fireDue(r.clk.now())
	_, ok := r.take("sess-flip")
	require.True(t, ok, "next tick on the anchor grid")
}

func TestSchedDet_OffIsInert(t *testing.T) {
	r := newRig(t, DefaultCadence, false)
	r.add(t, "sess-off")
	r.clk.advance(10 * time.Hour)
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen())
	require.Zero(t, r.w.firedOf("sess-off"))
	_, armed := r.w.nextTick(r.clk.now())
	require.False(t, armed, "no timer armed")
	r.w.fromOutput("sess-off")
	require.Equal(t, 1, r.w.pendingLen(), "output-driven requests as before")
	r.handle("sess-off", pendingReq{since: t0, late: true}) // a stray late flag does nothing while off
	require.False(t, progressSys(r.f.system(0)))
}

// A tick that lands during the session's own call merges with it; when that
// call yields nothing, one request is queued again.
func TestSchedDet_TickMergedIntoInFlightCallIsRearmedWhenCallYieldsNothing(t *testing.T) {
	enter, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(enter)
		<-release
		_, _ = w.Write([]byte(`{"response":""}`)) // cleans to nothing
	}))
	t.Cleanup(srv.Close)
	st := state.NewStore()
	w := New(schedCfg(srv.URL, DefaultCadence, true), st, uniqueTranscript(nil, nil))
	clk := &fakeClock{t: t0}
	w.now = clk.now
	st.Subscribe(w.onEvent)
	addLive(t, st, "sess-fl", t0)
	w.mu.Lock()
	anchor := w.sched["sess-fl"].anchor
	w.mu.Unlock()

	done := make(chan struct{})
	go func() { defer close(done); w.handle(context.Background(), "sess-fl", pendingReq{since: t0}) }()
	<-enter
	clk.set(DefaultCadence.Due(anchor, 1)) // tick 1 comes due mid-call
	w.fireDue(clk.now())
	require.Zero(t, w.pendingLen(), "merged with the call in flight")
	require.Equal(t, 1, w.firedOf("sess-fl"))
	close(release)
	<-done
	require.Equal(t, 1, w.pendingLen(), "the call produced no description: one request queued again")
}

func TestSchedDet_TickMergedIntoCallThatSucceedsIsNotRepeated(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-ok")
	anchor := r.anchor("sess-ok")
	r.w.mu.Lock()
	m := &sessionMeta{inflight: true, callStart: t0}
	r.w.perSess["sess-ok"] = m
	r.w.mu.Unlock()
	r.clk.set(DefaultCadence.Due(anchor, 1))
	r.w.fireDue(r.clk.now())
	require.Zero(t, r.w.pendingLen())
	r.w.mu.Lock()
	require.True(t, m.merged)
	r.w.mu.Unlock()
}

func TestCadence_JitterIsDeterministicAndBounded(t *testing.T) {
	c := DefaultCadence
	seen := map[time.Duration]bool{}
	for _, id := range []string{"a", "b", "sess-1", "sess-2", "0123456789abcdef", "x", "y", "z", "q", "r"} {
		j := c.Jitter(id)
		require.Equal(t, j, c.Jitter(id), "deterministic")
		require.LessOrEqual(t, j.Abs(), c.EarlyEvery/20, "within 5%% of the early interval")
		seen[j] = true
	}
	require.Greater(t, len(seen), 1, "ids spread")
	require.Equal(t, c.EarlyEvery/20, c.maxJitter())
}

func TestSchedDet_SessionsStartedTogetherDoNotTickTogether(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	anchors := map[time.Time]bool{}
	for _, id := range []string{"sess-aaaa", "sess-bbbb", "sess-cccc", "sess-dddd", "sess-eeee"} {
		r.add(t, id)
		anchors[r.anchor(id)] = true
	}
	require.Greater(t, len(anchors), 1)
}

func TestSchedDet_TimerWaitIsCapped(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-cap")
	d, armed := r.w.nextTick(r.clk.now())
	require.True(t, armed)
	require.Equal(t, time.Minute, d, "the loop re-evaluates at least every minute")
}

func TestSchedDet_ClockJumpBackDoesNotStall(t *testing.T) {
	r := newRig(t, DefaultCadence, true)
	r.add(t, "sess-back")
	anchor := r.anchor("sess-back")
	r.clk.set(DefaultCadence.Due(anchor, 3))
	r.w.fireDue(r.clk.now())
	r.take("sess-back")
	require.Equal(t, 3, r.w.firedOf("sess-back"))

	r.clk.set(anchor.Add(7 * time.Minute)) // the wall clock jumps back to inside tick 1's period
	r.w.fireDue(r.clk.now())
	require.Equal(t, 1, r.w.firedOf("sess-back"), "the count follows the clock back")
	d, _ := r.w.nextTick(r.clk.now())
	require.LessOrEqual(t, d, time.Minute)
	r.clk.set(DefaultCadence.Due(anchor, 2))
	r.w.fireDue(r.clk.now())
	_, ok := r.take("sess-back")
	require.True(t, ok, "ticks resume on the grid instead of waiting for the old position")
}
