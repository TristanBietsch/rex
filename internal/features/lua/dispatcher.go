package lua

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EventHandler receives events from a Dispatcher. *Runtime implements it.
type EventHandler interface {
	// OnEventContext handles one event. ctx is canceled when the Dispatcher
	// closes; a handler must return promptly after that.
	OnEventContext(ctx context.Context, eventType string, data any) error
}

// DispatcherOptions configures a Dispatcher.
type DispatcherOptions struct {
	// QueueSize caps the events waiting for the handler (0 = DefaultQueueSize).
	QueueSize int
	// Logger receives drop warnings. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// sessionUpdated is the normalized name of the only event type that coalesces.
const sessionUpdated = "session_updated"

// queuedEvent is one pending delivery. data is replaced in place while a
// session_updated event is still queued.
type queuedEvent struct {
	eventType string
	sessionID string
	data      any
}

// Dispatcher delivers daemon events to an EventHandler (a *Runtime) from a
// bounded queue on one goroutine.
//
// Store subscribers run synchronously inside Store.broadcast, often on a
// PTY reader goroutine, so they must never wait for Lua. Enqueue only touches
// the queue under a short mutex and never blocks:
//   - a session_updated event for a session whose previous update is still
//     queued replaces that update's payload in place, so a busy session holds
//     one slot and its handlers see the latest state;
//   - when the queue is full, the new event is dropped and counted, and a
//     warning is logged at most once per dropWarnInterval.
//
// Events for one session are delivered in the order they were enqueued;
// coalescing never moves an update past another event for that session.
// Enqueue, Dropped and Close are safe for concurrent use.
type Dispatcher struct {
	h    EventHandler
	log  *slog.Logger
	size int

	// ctx is the parent of every handler call; Close cancels it.
	ctx       context.Context
	cancel    context.CancelFunc
	wake      chan struct{} // cap 1: the queue may be non-empty
	done      chan struct{} // closed when the goroutine exits
	closeOnce sync.Once

	mu       sync.Mutex
	queue    []*queuedEvent
	pending  map[string]*queuedEvent // queued session_updated events by session ID
	closed   bool
	dropped  uint64    // total drops
	unwarned uint64    // drops since the last warning
	lastWarn time.Time // zero until the first warning
}

// NewDispatcher starts the delivery goroutine. Call Close to stop it.
func NewDispatcher(h EventHandler, opts DispatcherOptions) *Dispatcher {
	if opts.QueueSize <= 0 {
		opts.QueueSize = DefaultQueueSize
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{
		h:       h,
		log:     opts.Logger,
		size:    opts.QueueSize,
		ctx:     ctx,
		cancel:  cancel,
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		pending: make(map[string]*queuedEvent),
	}
	go d.run()
	return d
}

// Enqueue queues an event for the handler without blocking. eventType takes
// the same names as Runtime.OnEvent. It reports false when the event was
// dropped (queue full) or the Dispatcher is closed.
func (d *Dispatcher) Enqueue(eventType, sessionID string, data any) bool {
	key := normaliseEventType(eventType)

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return false
	}
	if key == sessionUpdated {
		if ev, ok := d.pending[sessionID]; ok {
			ev.data = data
			d.mu.Unlock()
			return true
		}
	}
	if len(d.queue) >= d.size {
		warn, since, total := d.noteDropLocked(time.Now())
		d.mu.Unlock()
		if warn {
			d.log.Warn("lua: event queue full; dropping events",
				"event", key, "session", sessionID, "dropped", since, "dropped_total", total, "queue_size", d.size)
		}
		return false
	}
	ev := &queuedEvent{eventType: key, sessionID: sessionID, data: data}
	d.queue = append(d.queue, ev)
	if key == sessionUpdated {
		d.pending[sessionID] = ev
	} else {
		// A later update must queue behind this event, not merge before it.
		delete(d.pending, sessionID)
	}
	d.mu.Unlock()

	select {
	case d.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
	return true
}

// Dropped returns how many events have been dropped because the queue was full.
func (d *Dispatcher) Dropped() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

// Close cancels the handler call in flight, discards queued events and
// waits for the delivery goroutine to exit. It is idempotent; Enqueue
// returns false afterwards.
func (d *Dispatcher) Close() {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.queue, d.pending = nil, nil
		dropped := d.dropped
		d.mu.Unlock()
		d.cancel()
		if dropped > 0 {
			d.log.Warn("lua: dispatcher stopped", "dropped_total", dropped)
		}
	})
	<-d.done
}

// noteDropLocked counts a dropped event and reports whether to warn now,
// with the drops since the last warning and the total. Called with d.mu held.
func (d *Dispatcher) noteDropLocked(now time.Time) (warn bool, since, total uint64) {
	d.dropped++
	d.unwarned++
	if !d.lastWarn.IsZero() && now.Sub(d.lastWarn) < dropWarnInterval {
		return false, 0, d.dropped
	}
	since = d.unwarned
	d.unwarned = 0
	d.lastWarn = now
	return true, since, d.dropped
}

// run delivers events one at a time until Close.
func (d *Dispatcher) run() {
	defer close(d.done)
	for {
		ev, ok := d.next()
		if !ok {
			return
		}
		// The handler logs its own failures; ErrClosed after the runtime
		// shut down just means nothing is listening any more.
		_ = d.h.OnEventContext(d.ctx, ev.eventType, ev.data)
	}
}

// next blocks until an event is queued or the Dispatcher closes. The event
// leaves pending as it is dequeued, so later updates queue a new delivery.
func (d *Dispatcher) next() (queuedEvent, bool) {
	for {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return queuedEvent{}, false
		}
		if len(d.queue) > 0 {
			ev := d.queue[0]
			d.queue[0] = nil
			d.queue = d.queue[1:]
			if d.pending[ev.sessionID] == ev {
				delete(d.pending, ev.sessionID)
			}
			out := *ev
			d.mu.Unlock()
			return out, true
		}
		d.mu.Unlock()

		select {
		case <-d.wake:
		case <-d.ctx.Done():
			return queuedEvent{}, false
		}
	}
}
