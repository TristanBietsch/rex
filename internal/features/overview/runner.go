package overview

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// ErrUnavailable is what a generate func returns while the model backend is
// down. The runner then keeps its request pending until the next Notify (the
// daemon sends one when the backend comes back) instead of retrying.
var ErrUnavailable = errors.New("overview: backend unavailable")

// Config times the runner.
type Config struct {
	// Debounce is how long a structural change waits, so a burst is one call.
	Debounce time.Duration
	// MinInterval is the least time between two model calls, and the back-off
	// after a failed one.
	MinInterval time.Duration
	// Refresh is the most often a description-only change runs the model.
	Refresh time.Duration
	// Timeout bounds one call, including the wait for the shared Ollama slot.
	Timeout time.Duration
}

// DefaultConfig is the production timing: about two calls a minute at most.
func DefaultConfig() Config {
	return Config{Debounce: 3 * time.Second, MinInterval: 30 * time.Second, Refresh: 2 * time.Minute, Timeout: 45 * time.Second}
}

// Runner keeps the board's overview headline current. It owns no model, store
// or server: those come in as closures, so the package stays pure.
//
// The runner only displays: its output never drives marking, sending or
// retiring. Debug logs carry counts and durations, never the digest or the
// headline.
type Runner struct {
	cfg      Config
	snapshot func() []protocol.SessionSummary
	generate func(ctx context.Context, digest string) (string, error)
	publish  func(protocol.Overview)
	active   func() bool

	wake chan struct{} // capacity 1

	mu          sync.Mutex
	enabled     bool
	epoch       uint64 // bumped on every SetEnabled change; drops results of an older epoch
	structural  bool
	structAt    time.Time
	content     bool
	contentAt   time.Time
	parked      bool // waiting for a Notify: backend down or no viewer
	lastCall    time.Time
	lastSuccess time.Time
	done        Prints // fingerprints of the last published headline
	haveDone    bool
	emptyShown  bool // the idle-board overview is already published
}

// NewRunner builds a disabled runner. snapshot returns the board, generate
// turns a digest into a headline (returning ErrUnavailable while the backend
// is down), publish delivers an overview and active reports whether anyone is
// looking. All four are required. publish and active must not call back into
// the runner.
func NewRunner(cfg Config, snapshot func() []protocol.SessionSummary,
	generate func(ctx context.Context, digest string) (string, error),
	publish func(protocol.Overview), active func() bool) *Runner {
	if snapshot == nil || generate == nil || publish == nil || active == nil {
		panic("overview: NewRunner needs snapshot, generate, publish and active")
	}
	return &Runner{cfg: cfg, snapshot: snapshot, generate: generate, publish: publish, active: active,
		wake: make(chan struct{}, 1)}
}

// Notify records that the board changed. It never blocks, so it is safe inside
// a store subscription, and does nothing while the runner is disabled.
func (r *Runner) Notify(rel Relevance) {
	if rel == Ignored {
		return
	}
	r.mu.Lock()
	if !r.enabled {
		r.mu.Unlock()
		return
	}
	now := time.Now()
	if rel == StructuralChange && !r.structural {
		r.structural, r.structAt = true, now
	}
	if rel == ContentChange && !r.content {
		r.content, r.contentAt = true, now
	}
	r.parked = false
	r.mu.Unlock()
	r.poke()
}

// SetEnabled turns the runner on or off. Turning it on publishes
// {Enabled:true} and schedules a first headline; turning it off publishes
// {Enabled:false} and drops everything pending, so a call in flight is
// discarded. Setting the current value does nothing.
func (r *Runner) SetEnabled(on bool) {
	r.mu.Lock()
	if r.enabled == on {
		r.mu.Unlock()
		return
	}
	r.enabled = on
	r.epoch++
	r.structural, r.content, r.parked = false, false, false
	// {Enabled:true} with no headline is also what an idle board publishes.
	r.haveDone, r.emptyShown = false, on
	if on {
		r.structural, r.structAt = true, time.Now()
	}
	r.publish(protocol.Overview{Enabled: on})
	r.mu.Unlock()
	r.poke()
}

func (r *Runner) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run serves until ctx ends. Start it once. While the runner is disabled it
// only waits.
func (r *Runner) Run(ctx context.Context) error {
	for {
		wait, ok := r.nextWait()
		if !ok {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-r.wake:
			}
			continue
		}
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-r.wake: // re-plan: the board changed again
				t.Stop()
				continue
			case <-t.C:
			}
		}
		r.runOnce(ctx)
	}
}

// nextWait plans the next call: how long to wait, and false when there is
// nothing to do until the next Notify or SetEnabled.
func (r *Runner) nextWait() (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled || r.parked || (!r.structural && !r.content) {
		return 0, false
	}
	var due time.Time
	if r.structural {
		due = r.structAt.Add(r.cfg.Debounce)
	} else {
		due = later(r.contentAt, r.lastSuccess.Add(r.cfg.Refresh))
	}
	due = later(due, r.lastCall.Add(r.cfg.MinInterval))
	return max(time.Until(due), 0), true
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// runOnce makes one pass: it publishes the idle overview, skips an unchanged
// board, or calls the model.
func (r *Runner) runOnce(ctx context.Context) {
	r.mu.Lock()
	if !r.enabled {
		r.mu.Unlock()
		return
	}
	epoch := r.epoch
	if !r.active() {
		r.parked = true // a Notify (a viewer's Hello sends one) wakes it
		r.mu.Unlock()
		slog.Debug("overview: no viewer, parked")
		return
	}
	// Whatever was pending is covered by this pass.
	r.structural, r.content = false, false
	r.mu.Unlock()

	ss := r.snapshot()
	prints := Fingerprint(ss)
	groups := Group(ss)
	if len(groups[NeedsYou])+len(groups[Review])+len(groups[Failed])+len(groups[Working]) == 0 {
		r.publishIf(epoch, func() {
			if !r.emptyShown {
				r.publish(protocol.Overview{Enabled: true})
			}
			r.emptyShown, r.done, r.haveDone = true, prints, false
		})
		return
	}
	r.mu.Lock()
	unchanged := r.haveDone && r.done.Content == prints.Content
	r.mu.Unlock()
	if unchanged {
		slog.Debug("overview: board unchanged, no call")
		return
	}

	digest := Digest(ss, time.Now())
	callCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	start := time.Now()
	headline, err := r.generate(callCtx, digest)
	cancel()
	slog.Debug("overview: generate", "sessions", len(ss), "digest_bytes", len(digest),
		"ms", time.Since(start).Milliseconds(), "ok", err == nil)

	r.mu.Lock()
	r.lastCall = time.Now()
	r.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		return
	case errors.Is(err, ErrUnavailable):
		r.mu.Lock()
		if r.epoch == epoch {
			r.structural, r.structAt = true, time.Now()
			r.parked = true // until the backend's health flips back and Notify runs
		}
		r.mu.Unlock()
	case err != nil:
		r.mu.Lock()
		if r.epoch == epoch {
			r.structural, r.structAt = true, time.Now() // retried after MinInterval
		}
		r.mu.Unlock()
	default:
		now := time.Now()
		r.publishIf(epoch, func() {
			r.publish(protocol.Overview{Enabled: true, Headline: headline, GeneratedAt: now})
			r.done, r.haveDone, r.emptyShown, r.lastSuccess = prints, true, false, now
		})
	}
}

// publishIf runs fn under the lock unless the runner was toggled since epoch
// began: a result computed before a disable must not reach clients after it.
func (r *Runner) publishIf(epoch uint64, fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.epoch == epoch && r.enabled {
		fn()
	}
}
