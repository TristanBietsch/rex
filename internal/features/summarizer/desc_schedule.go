package summarizer

import (
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

// schedEntry is the schedule of one live session: its run's start and how
// many ticks have come due so far (fired, counted whether or not a call was
// made).
type schedEntry struct {
	anchor time.Time
	fired  int
}

// SetSchedule turns the scheduled refresh (the summary_schedule setting) on
// or off. Safe from any goroutine; it applies to the next loop pass. Turning
// it on counts the ticks already past as fired, so the next tick is the next
// one on each session's own grid; nothing is replayed. Turning it off drops
// ticks at once and honors output-driven requests again.
func (w *Worker) SetSchedule(on bool) {
	// One critical section: fireDue reads the flag under the same lock, so it
	// never sees "on" with stale counts.
	w.mu.Lock()
	if w.schedOn.Load() == on {
		w.mu.Unlock()
		return
	}
	if on {
		now := w.now()
		for _, e := range w.sched {
			e.fired = w.cadence.Elapsed(e.anchor, now)
		}
	}
	w.schedOn.Store(on)
	w.mu.Unlock()
	slog.Info("summarizer: schedule", "on", on)
	w.wakeup()
}

// Schedule reports whether the scheduled refresh is on.
func (w *Worker) Schedule() bool { return w.schedOn.Load() }

// seedSchedule tracks the sessions already live when the worker starts, from
// their StartedAt. Ticks already past count as fired.
func (w *Worker) seedSchedule() {
	for _, sess := range w.store.All() {
		sum := sess.Summary()
		if isTerminal(sum.State) {
			continue
		}
		w.track(sum.ID, sum.StartedAt)
	}
}

// track starts the schedule of a live session for a run that began at start,
// unless it already has one. The grid is shifted by the session's jitter, and
// ticks already due count as fired (no catch-up burst).
func (w *Worker) track(id string, start time.Time) {
	w.mu.Lock()
	_, ok := w.sched[id]
	if !ok {
		anchor := start.Add(w.cadence.Jitter(id))
		w.sched[id] = &schedEntry{anchor: anchor, fired: w.cadence.Elapsed(anchor, w.now())}
	}
	w.mu.Unlock()
	if !ok && w.schedOn.Load() {
		w.wakeup() // the loop re-arms its timer for the new session's tick
	}
}

// restart begins a new run's schedule for id at the current time: the early
// phase again.
func (w *Worker) restart(id string) {
	w.mu.Lock()
	delete(w.sched, id)
	w.mu.Unlock()
	w.track(id, w.now())
}

// trackEvent keeps the schedule's live set in step with the store: a new live
// session is anchored at its StartedAt; a session reopened (Resume, undoing a
// finished state: EventReopened) restarts its early phase; a session that
// goes live by a state change with no schedule yet is anchored now; a
// finished or removed one is dropped. It runs on the store's goroutines, so it
// only takes the mutex.
func (w *Worker) trackEvent(e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil && !isTerminal(e.Summary.State) {
			w.track(e.SessionID, e.Summary.StartedAt)
		}
	case state.EventReopened:
		w.restart(e.SessionID)
	case state.EventUpdated:
		if e.NewState == nil {
			return
		}
		if isTerminal(*e.NewState) {
			w.mu.Lock()
			delete(w.sched, e.SessionID)
			w.mu.Unlock()
			return
		}
		w.track(e.SessionID, w.now())
	case state.EventRemoved:
		// forget drops the schedule with the rest of the session's state.
	}
}

// fromOutput handles one send on the worker's channel (the supervisor's
// output-driven trigger). With the schedule on, descriptions refresh on the
// clock and on state changes only, so the send is dropped, except while the
// session has no description yet: the first state-change refresh can run
// before the agent printed anything, and the board should not show a bare
// title until tick 1.
func (w *Worker) fromOutput(id string) {
	if w.schedOn.Load() {
		if sess, ok := w.store.Get(id); !ok || sess.Summary().Description != "" {
			return
		}
	}
	w.enqueue(id, time.Now(), false)
}

// fireDue enqueues a refresh for every session whose tick has come due since
// the last pass. Ticks missed in one stretch (a long call, a backend that was
// down, a clock jump forward) are caught up in one step into a single pending
// request; a clock that jumped back pulls the count back with it. A tick that
// came due while the session's own call was running is counted and merged
// with that call (re-queued once if the call yields nothing). It never
// blocks.
func (w *Worker) fireDue(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.schedOn.Load() {
		return
	}
	for id, e := range w.sched {
		k := w.cadence.Elapsed(e.anchor, now)
		if k < e.fired {
			e.fired = k // the clock went back: follow it
			continue
		}
		if k == e.fired {
			continue
		}
		e.fired = k
		late := w.cadence.Late(k)
		due := w.cadence.Due(e.anchor, k)
		if m := w.perSess[id]; m != nil && (m.inflight || (!due.Before(m.callStart) && !due.After(m.callEnd))) {
			slog.Debug("summarizer: tick merged with call", "session", id, "tick", k)
			if m.inflight {
				m.merged, m.mergedLate = true, late
			}
			continue
		}
		slog.Debug("summarizer: tick", "session", id, "tick", k, "late", late)
		w.mergeLocked(id, pendingReq{since: now, late: late})
	}
}

// nextTick is the time until the earliest session's next tick, at most
// maxTickWait; ok is false when the schedule is off or no session is live
// (nothing to arm).
func (w *Worker) nextTick(now time.Time) (d time.Duration, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.schedOn.Load() {
		return 0, false
	}
	for _, e := range w.sched {
		t := w.cadence.Due(e.anchor, e.fired+1).Sub(now)
		if !ok || t < d {
			d, ok = t, true
		}
	}
	if ok {
		d = min(max(d, time.Nanosecond), maxTickWait)
	}
	return d, ok
}
