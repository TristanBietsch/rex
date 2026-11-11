package summarizer

import (
	"context"
	"time"
)

// pendingReq is one coalesced summary request. Any number of sends for the
// same session collapse into a single entry.
type pendingReq struct {
	since time.Time // when the oldest merged request arrived (fairness key)
	prio  bool      // a state change: runs before ordinary requests
}

// enqueue records a request for id, merging with a pending one: the earliest
// since wins and prio is sticky. Safe from any goroutine.
func (w *Worker) enqueue(id string, since time.Time, prio bool) {
	w.mu.Lock()
	cur, ok := w.pending[id]
	if !ok || since.Before(cur.since) {
		cur.since = since
	}
	cur.prio = cur.prio || prio
	w.pending[id] = cur
	w.mu.Unlock()
}

// wakeup nudges the scheduler loop; never blocks, and coalesces.
func (w *Worker) wakeup() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// drain moves everything queued on w.ch into pending without blocking.
func (w *Worker) drain() {
	for {
		select {
		case id := <-w.ch:
			w.enqueue(id, time.Now(), false)
		default:
			return
		}
	}
}

// next picks the pending request to run: eligible ones only (the session's
// last call was at least MinInterval ago), priority first, then oldest. When
// none is eligible it returns the time until the earliest becomes so (0 when
// nothing is pending or the backend is down, meaning "wait for a nudge").
// The chosen entry is removed from pending.
func (w *Worker) next(now time.Time) (id string, req pendingReq, wait time.Duration, ok bool) {
	if !w.BackendAvailable() {
		return "", pendingReq{}, 0, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var (
		best    string
		bestReq pendingReq
		found   bool
		soonest time.Duration
	)
	for cid, r := range w.pending {
		if m := w.perSess[cid]; m != nil {
			if d := w.cfg.MinInterval - now.Sub(m.lastSubmittedAt); d > 0 {
				if soonest == 0 || d < soonest {
					soonest = d
				}
				continue
			}
		}
		better := !found ||
			(r.prio && !bestReq.prio) ||
			(r.prio == bestReq.prio && r.since.Before(bestReq.since))
		if better {
			best, bestReq, found = cid, r, true
		}
	}
	if !found {
		return "", pendingReq{}, soonest, false
	}
	delete(w.pending, best)
	return best, bestReq, 0, true
}

// loop is the scheduler: it drains requests, runs the next eligible one, and
// otherwise sleeps until a send, a nudge, or the earliest eligibility.
func (w *Worker) loop(ctx context.Context) error {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.drain()
		id, req, wait, ok := w.next(time.Now())
		if ok {
			w.handle(ctx, id, req)
			continue
		}
		var tick <-chan time.Time
		if wait > 0 {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
			tick = timer.C
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-w.ch:
			w.enqueue(id, time.Now(), false)
		case <-w.wake:
		case <-tick:
		}
	}
}

// forget drops everything held for a removed session.
func (w *Worker) forget(id string) {
	w.mu.Lock()
	delete(w.perSess, id)
	delete(w.pending, id)
	w.mu.Unlock()
}

// acquireSlot takes the single Ollama slot: every call to the backend,
// whether from the loop or from Summarize, holds it, so a local model never
// serves two requests at once. The returned func releases it.
func (w *Worker) acquireSlot(ctx context.Context) (release func(), err error) {
	select {
	case w.slot <- struct{}{}:
		return func() { <-w.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
