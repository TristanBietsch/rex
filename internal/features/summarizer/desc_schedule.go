package summarizer

import (
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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
	if w.schedOn.Swap(on) == on {
		return
	}
	if on {
		now := w.now()
		w.mu.Lock()
		for _, e := range w.sched {
			e.fired = w.cadence.Elapsed(e.anchor, now)
		}
		w.mu.Unlock()
	}
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
		w.track(sum.ID, sum.StartedAt, true)
	}
}

// track starts the schedule of a live session at anchor, unless it already
// has one. past counts the ticks already due as fired.
func (w *Worker) track(id string, anchor time.Time, past bool) {
	w.mu.Lock()
	_, ok := w.sched[id]
	if !ok {
		e := &schedEntry{anchor: anchor}
		if past {
			e.fired = w.cadence.Elapsed(anchor, w.now())
		}
		w.sched[id] = e
	}
	w.mu.Unlock()
	if !ok && w.schedOn.Load() {
		w.wakeup() // the loop re-arms its timer for the new session's tick
	}
}

// eventState is the state a store event moves a session to: NewState, or the
// "state" key of a patch (a Resume announces the reopen that way).
func eventState(e state.Event) (protocol.State, bool) {
	if e.NewState != nil {
		return *e.NewState, true
	}
	switch v := e.Patch["state"].(type) {
	case protocol.State:
		return v, true
	case string:
		return protocol.State(v), true
	}
	return "", false
}

// trackEvent keeps the schedule's live set in step with the store: a new live
// session is anchored at its StartedAt, a session that goes live from a
// finished state (Resume, undoing done) is anchored now, which restarts the
// early phase, and a finished or removed one is dropped. It runs on the
// store's goroutines, so it only takes the mutex.
func (w *Worker) trackEvent(e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil && !isTerminal(e.Summary.State) {
			w.track(e.SessionID, e.Summary.StartedAt, false)
		}
	case state.EventUpdated:
		st, ok := eventState(e)
		if !ok {
			return
		}
		if isTerminal(st) {
			w.mu.Lock()
			delete(w.sched, e.SessionID)
			w.mu.Unlock()
			return
		}
		w.track(e.SessionID, w.now(), false)
	}
}

// fromOutput handles one send on the worker's channel (the supervisor's
// output-driven trigger). With the schedule on, descriptions refresh on the
// clock and on state changes only, so the send is dropped.
func (w *Worker) fromOutput(id string) {
	if w.schedOn.Load() {
		return
	}
	w.enqueue(id, time.Now(), false)
}

// fireDue enqueues a refresh for every session whose tick has come due since
// the last pass. Ticks missed in one stretch (a long call, a backend that was
// down) are caught up in one step into a single pending request. A tick that
// came due while the session's own call was running is counted and merged
// with that call. It never blocks.
func (w *Worker) fireDue(now time.Time) {
	if !w.schedOn.Load() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, e := range w.sched {
		k := w.cadence.Elapsed(e.anchor, now)
		if k <= e.fired {
			continue
		}
		e.fired = k
		due := w.cadence.Due(e.anchor, k)
		if m := w.perSess[id]; m != nil && !due.Before(m.callStart) && !due.After(m.callEnd) {
			slog.Debug("summarizer: tick merged with call", "session", id, "tick", k)
			continue
		}
		late := w.cadence.Late(k)
		slog.Debug("summarizer: tick", "session", id, "tick", k, "late", late)
		w.mergeLocked(id, pendingReq{since: now, late: late})
	}
}

// nextTick is the time until the earliest session's next tick; ok is false
// when the schedule is off or no session is live (nothing to arm).
func (w *Worker) nextTick(now time.Time) (d time.Duration, ok bool) {
	if !w.schedOn.Load() {
		return 0, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range w.sched {
		t := w.cadence.Due(e.anchor, e.fired+1).Sub(now)
		if !ok || t < d {
			d, ok = t, true
		}
	}
	if ok && d <= 0 {
		d = time.Nanosecond
	}
	return d, ok
}
