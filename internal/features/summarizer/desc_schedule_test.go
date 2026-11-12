package summarizer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// recOllama is an httptest Ollama that records every request's system prompt
// and time, and answers with a fixed description.
type recOllama struct {
	srv *httptest.Server
	mu  sync.Mutex
	sys []string
	at  []time.Time
}

func newRecOllama(t *testing.T) *recOllama {
	t.Helper()
	r := &recOllama{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var cr struct {
			System string `json:"system"`
		}
		_ = json.Unmarshal(body, &cr)
		r.mu.Lock()
		r.sys = append(r.sys, cr.System)
		r.at = append(r.at, time.Now())
		r.mu.Unlock()
		_, _ = io.WriteString(w, `{"response":"Running tests"}`)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *recOllama) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sys)
}

func (r *recOllama) system(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sys[i]
}

func (r *recOllama) time(i int) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at[i]
}

// progressSys reports whether a recorded system prompt is the late-phase one.
func progressSys(s string) bool { return strings.Contains(s, "finished so far") }

func addLive(t *testing.T, st *state.Store, id string, startedAt time.Time) {
	t.Helper()
	require.NoError(t, st.Add(&state.Session{
		ID: id, ShortID: id[:4], ToolID: "codex", Slug: "task-" + id,
		State: protocol.StateWorking, StartedAt: startedAt,
	}))
}

func schedCfg(url string, c Cadence, on bool) Config {
	cfg := Defaults()
	cfg.BaseURL = url
	cfg.MinInterval = time.Millisecond // New maps 0 to its 800 ms default
	cfg.Cadence = c
	cfg.Schedule = on
	return cfg
}

// startedWorker starts an already built worker and stops it with the test.
func startedWorker(t *testing.T, w *Worker) *Worker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Start(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	<-w.subscribed
	return w
}

func (w *Worker) pendingLen() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending)
}

func (w *Worker) schedLen() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.sched)
}

func (w *Worker) firedOf(id string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sched[id].fired
}

// Ticks land on the cadence grid anchored to StartedAt, not to when the
// worker started or when the last call finished. A tick already past when the
// worker starts counts as fired and does not run.
func TestSchedule_TicksAnchoredToStartedAt(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 100 * time.Millisecond, EarlyCount: 3, LateEvery: 200 * time.Millisecond}
	st := state.NewStore()
	anchor := time.Now().Add(-150 * time.Millisecond) // tick 1 (100 ms) is already past
	addLive(t, st, "sess-anch", anchor)
	startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	// Remaining ticks: 200, 300 (early), 500, 700 (late) after the anchor.
	require.Eventually(t, func() bool { return f.count() >= 4 }, 3*time.Second, 5*time.Millisecond)
	for i, k := range []int{2, 3, 4, 5} {
		due := c.Due(anchor, k)
		got := f.time(i)
		require.False(t, got.Before(due.Add(-5*time.Millisecond)), "call %d (tick %d) came %v early", i, k, due.Sub(got))
		require.Less(t, got.Sub(due), 120*time.Millisecond, "call %d (tick %d) came late", i, k)
	}
	require.False(t, progressSys(f.system(0)) || progressSys(f.system(1)), "ticks 2 and 3 are early phase")
	require.True(t, progressSys(f.system(2)) && progressSys(f.system(3)), "ticks 4 and 5 are late phase")
}

// A state change refreshes at once and leaves the schedule where it was.
func TestSchedule_StateChangeImmediateAndDoesNotShiftTicks(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 300 * time.Millisecond, EarlyCount: 5, LateEvery: time.Hour}
	st := state.NewStore()
	anchor := time.Now()
	addLive(t, st, "sess-shift", anchor)
	startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	time.Sleep(60 * time.Millisecond)
	require.NoError(t, st.Transition("sess-shift", protocol.StateNeedsInput))
	require.Eventually(t, func() bool { return f.count() == 1 }, time.Second, 2*time.Millisecond)
	require.Less(t, f.time(0).Sub(anchor), 250*time.Millisecond, "state change runs immediately, not at the tick")

	require.Eventually(t, func() bool { return f.count() == 2 }, 2*time.Second, 5*time.Millisecond)
	tick := f.time(1)
	require.GreaterOrEqual(t, tick.Sub(anchor), 295*time.Millisecond)
	require.Less(t, tick.Sub(anchor), 420*time.Millisecond, "tick 1 stays at anchor+300ms; the state change did not shift it")
}

// While the flag is on, the supervisor's output-driven sends do nothing; with
// it off they are honored.
func TestSchedule_OutputSendsIgnoredOnHonoredOff(t *testing.T) {
	f := newRecOllama(t)
	st := state.NewStore()
	addLive(t, st, "sess-out1", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, DefaultCadence, true), st, uniqueTranscript(nil, nil))

	for range 200 { // more than the channel buffer: the loop drains, never blocks
		w.Channel() <- "sess-out1"
	}
	time.Sleep(150 * time.Millisecond)
	require.Zero(t, f.count(), "output-driven sends are dropped while summary_schedule is on")
	require.Zero(t, w.pendingLen())

	w.SetSchedule(false)
	w.Channel() <- "sess-out1"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
}

// An unchanged screen makes no call, but the tick still counts: the next one
// is the late phase.
func TestSchedule_UnchangedScreenSkipsCallButTickCounts(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 80 * time.Millisecond, EarlyCount: 2, LateEvery: 160 * time.Millisecond}
	st := state.NewStore()
	addLive(t, st, "sess-same", time.Now())
	startWorker(t, schedCfg(f.srv.URL, c, true), st, transcriptStub("the very same screen"))

	// tick 1 (80 ms): call. tick 2 (160 ms): unchanged, skipped but counted.
	// tick 3 (320 ms) is late: a different prompt, so a call. tick 4: skipped.
	require.Eventually(t, func() bool { return f.count() == 2 }, 3*time.Second, 5*time.Millisecond)
	require.False(t, progressSys(f.system(0)))
	require.True(t, progressSys(f.system(1)), "the skipped tick still counted, so tick 3 is late phase")
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 2, f.count(), "later ticks with an unchanged screen make no call")
}

// Early-phase and state-change requests use summary_detail; late-phase ticks
// use one level up and the progress prompt.
func TestSchedule_LatePhaseDetailUpAndProgressPrompt(t *testing.T) {
	for _, tc := range []struct {
		set        Detail
		early, lat string
	}{
		{DetailShort, "at most 60 characters", "at most 110 characters"},
		{DetailMedium, "at most 110 characters", "at most 180 characters"},
		{DetailLong, "at most 180 characters", "at most 180 characters"},
	} {
		t.Run(tc.set.String(), func(t *testing.T) {
			f := newRecOllama(t)
			c := Cadence{EarlyEvery: 60 * time.Millisecond, EarlyCount: 2, LateEvery: 120 * time.Millisecond}
			st := state.NewStore()
			addLive(t, st, "sess-late", time.Now())
			cfg := schedCfg(f.srv.URL, c, true)
			cfg.Detail = tc.set
			startWorker(t, cfg, st, uniqueTranscript(nil, nil))

			require.Eventually(t, func() bool { return f.count() >= 4 }, 3*time.Second, 5*time.Millisecond)
			for i := range 2 {
				require.False(t, progressSys(f.system(i)))
				require.Contains(t, f.system(i), tc.early, "early tick %d", i+1)
			}
			for i := 2; i < 4; i++ {
				require.True(t, progressSys(f.system(i)))
				require.Contains(t, f.system(i), tc.lat, "late tick %d", i-1)
			}
		})
	}
}

// A state-change refresh is never a progress request, even deep in the run.
func TestSchedule_StateChangeUsesActivityPromptAtSetting(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: time.Hour, EarlyCount: 0, LateEvery: time.Hour}
	st := state.NewStore()
	addLive(t, st, "sess-sc", time.Now().Add(-3*time.Hour)) // long past the early phase
	cfg := schedCfg(f.srv.URL, c, true)
	cfg.Detail = DetailShort
	startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	require.NoError(t, st.Transition("sess-sc", protocol.StateNeedsInput))
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	require.False(t, progressSys(f.system(0)))
	require.Contains(t, f.system(0), "at most 60 characters")
}

func TestSchedule_RemovedSessionGetsNoMoreTicks(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 50 * time.Millisecond, EarlyCount: 100, LateEvery: time.Hour}
	st := state.NewStore()
	addLive(t, st, "sess-rm", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	require.Eventually(t, func() bool { return f.count() >= 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, 1, w.schedLen())
	require.NoError(t, st.Remove("sess-rm"))
	require.Eventually(t, func() bool { return w.schedLen() == 0 && w.pendingLen() == 0 }, time.Second, 5*time.Millisecond)
	time.Sleep(60 * time.Millisecond) // let an in-flight call finish
	n := f.count()
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, n, f.count())
}

func TestSchedule_TerminalSessionGetsNoTicksButFinalSummary(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 50 * time.Millisecond, EarlyCount: 100, LateEvery: time.Hour}
	st := state.NewStore()
	addLive(t, st, "sess-fin", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	require.NoError(t, st.Transition("sess-fin", protocol.StateDone))
	require.Eventually(t, func() bool { return f.count() >= 1 && w.schedLen() == 0 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	n := f.count()
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, n, f.count(), "no scheduled refresh after done")
}

// While the backend is down ticks do not pile up: one pending request per
// session, run once when the backend returns.
func TestSchedule_BackendDownAtMostOnePending(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 40 * time.Millisecond, EarlyCount: 3, LateEvery: time.Hour}
	st := state.NewStore()
	addLive(t, st, "sess-down", time.Now())
	w := New(schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))
	w.MarkUnavailable("test")
	w = startedWorker(t, w)

	require.Eventually(t, func() bool { return w.firedOf("sess-down") == 3 }, 2*time.Second, 5*time.Millisecond)
	require.Zero(t, f.count())
	require.Equal(t, 1, w.pendingLen(), "three ticks, one pending request")

	w.MarkAvailable()
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, f.count(), "the merged ticks make one call")
}

// Resume restarts the early phase: the anchor is the reopen, not StartedAt.
func TestSchedule_ReopenRestartsEarlyPhase(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 80 * time.Millisecond, EarlyCount: 1, LateEvery: 160 * time.Millisecond}
	st := state.NewStore()
	addLive(t, st, "sess-reo", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	require.Eventually(t, func() bool { return f.count() >= 1 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, st.Transition("sess-reo", protocol.StateDone))
	require.Eventually(t, func() bool { return w.schedLen() == 0 }, time.Second, 5*time.Millisecond)
	time.Sleep(400 * time.Millisecond) // the old schedule would have ticked (late phase) twice by now
	n := f.count()
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, n, f.count())

	// What the server does on Resume: Reopen, then a state patch (no NewState).
	require.NoError(t, st.Reopen("sess-reo"))
	reopened := time.Now()
	sess, _ := st.Get("sess-reo")
	st.BroadcastPatch("sess-reo", map[string]any{"state": sess.Summary().State})

	require.Eventually(t, func() bool { return f.count() > n }, 2*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return w.schedLen() == 1 }, time.Second, 5*time.Millisecond)
	// The first call after the reopen is the early tick, 80 ms after it.
	first := f.time(n)
	require.GreaterOrEqual(t, first.Sub(reopened), 75*time.Millisecond)
	require.False(t, progressSys(f.system(n)), "the early phase restarted")
}

func TestSchedule_FlagFlipLive(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 50 * time.Millisecond, EarlyCount: 1000, LateEvery: time.Hour}
	st := state.NewStore()
	anchor := time.Now()
	addLive(t, st, "sess-flip", anchor)
	w := startWorker(t, schedCfg(f.srv.URL, c, true), st, uniqueTranscript(nil, nil))

	require.Eventually(t, func() bool { return f.count() >= 2 }, 2*time.Second, 5*time.Millisecond)
	w.SetSchedule(false)
	time.Sleep(100 * time.Millisecond) // an in-flight call finishes
	n := f.count()
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, n, f.count(), "ticks stop at once when the flag goes off")

	w.SetSchedule(true)
	require.Eventually(t, func() bool { return f.count() > n }, 2*time.Second, 5*time.Millisecond)
	// The ticks missed while off are not replayed: one call, then the grid.
	require.Less(t, f.count(), n+3)
	require.GreaterOrEqual(t, w.firedOf("sess-flip"), c.Elapsed(anchor, time.Now())-1)
}

// Off is today's behavior and inert: no tick is computed or armed.
func TestSchedule_OffIsInert(t *testing.T) {
	f := newRecOllama(t)
	c := Cadence{EarlyEvery: 20 * time.Millisecond, EarlyCount: 1000, LateEvery: time.Hour}
	st := state.NewStore()
	addLive(t, st, "sess-off", time.Now())
	w := startWorker(t, schedCfg(f.srv.URL, c, false), st, uniqueTranscript(nil, nil))

	time.Sleep(250 * time.Millisecond)
	require.Zero(t, f.count(), "no scheduled refresh")
	require.Zero(t, w.firedOf("sess-off"))
	_, ok := w.nextTick(time.Now())
	require.False(t, ok, "no tick armed")

	// Output-driven refreshes work as before, with the base prompt.
	w.Channel() <- "sess-off"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	require.False(t, progressSys(f.system(0)))
}
