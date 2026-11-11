package usage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Ledger timing.
const (
	// FlushEvery is the least time between two ledger lines of one live
	// session.
	FlushEvery = 10 * time.Minute
	// Retain is how far back the ledger is kept (trimmed on daemon start).
	Retain = 90 * 24 * time.Hour
	// tickEvery is how often the hub looks for live sessions to snapshot.
	tickEvery = time.Minute
)

// HubConfig wires a Hub. Zero timing fields take the defaults above.
type HubConfig struct {
	Ledger *Ledger
	// Sessions lists the board's sessions; the hub reads it each time it is
	// switched on, to pick up sessions that changed while it was off (nil:
	// none).
	Sessions SessionLister
	// Now is the clock (nil = time.Now); tests inject it.
	Now func() time.Time
	// Tick, FlushEvery and Retain override the defaults (tests).
	Tick, FlushEvery, Retain time.Duration
	// OnUsage is called with the new Usage state when a quota changed or the
	// hub was switched on or off. It must not block (the server fans it out
	// without I/O). nil: nobody listens.
	OnUsage func(protocol.Usage)
}

// Hub is the daemon's usage state: the live sessions' totals and the ledger
// lines they become.
//
// It is inert while disabled: no goroutine, no file write, no ledger read,
// and every method returns at once. The first SetEnabled(true) trims the
// ledger and starts the one goroutine; it then lives until Close, parked
// while disabled. The caller feeds it session changes (Observe, Removed),
// usually from a store subscription, so those never block or touch a file:
// they only queue ledger lines, which the goroutine writes.
type Hub struct {
	ctx    context.Context
	cancel context.CancelFunc
	cfg    HubConfig
	once   sync.Once
	done   chan struct{}
	wake   chan struct{} // cap 1: the queue has lines

	wmu       sync.Mutex // serializes drain: the queue is written in order, once
	publishMu sync.Mutex // orders OnUsage calls (see publishAndUnlock)

	mu      sync.Mutex
	enabled bool
	started bool
	live    map[string]*liveSession
	prior   map[string]Record // the ledger's latest line per session at first enable
	queue   []Record          // lines waiting for the goroutine to write
	quotas  map[quotaKey]protocol.UsageQuota
}

// liveSession is what the hub knows about one session still on the board.
type liveSession struct {
	sum        protocol.SessionSummary
	costs      map[string]float64 // cost.total_cost_usd by Claude conversation id
	hasCost    bool
	model      string  // the model the tool reported, "" = sum.ModelID
	seedCost   float64 // cost from before this daemon started
	lastWrite  time.Time
	lastRec    Record
	wroteFinal bool
}

// NewHub builds a disabled hub. ctx bounds its goroutine.
func NewHub(ctx context.Context, cfg HubConfig) *Hub {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Tick <= 0 {
		cfg.Tick = tickEvery
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = FlushEvery
	}
	if cfg.Retain <= 0 {
		cfg.Retain = Retain
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Hub{ctx: ctx, cancel: cancel, cfg: cfg, done: make(chan struct{}), wake: make(chan struct{}, 1),
		live: map[string]*liveSession{}, prior: map[string]Record{}}
}

// SetEnabled switches the hub on or off. The first switch on trims the
// ledger to Retain, reads its latest lines and starts the goroutine; every
// switch on then looks at the board once (HubConfig.Sessions).
func (h *Hub) SetEnabled(on bool) {
	h.mu.Lock()
	was := h.enabled
	h.enabled = on
	if !on {
		clear(h.live)
		h.queue = nil
		h.quotas = nil
	}
	if on != was {
		h.publishAndUnlock(protocol.Usage{Enabled: on})
	} else {
		h.mu.Unlock()
	}
	if !on || was {
		return
	}
	h.once.Do(func() {
		if err := h.cfg.Ledger.Trim(h.cfg.Now().Add(-h.cfg.Retain)); err != nil {
			slog.Warn("usage: ledger trim failed", "err", err)
		}
		prior, err := h.cfg.Ledger.Latest()
		if err != nil {
			slog.Warn("usage: ledger read failed", "err", err)
		}
		h.mu.Lock()
		if prior != nil {
			h.prior = prior
		}
		h.started = true
		h.mu.Unlock()
		go h.run()
	})
	if h.cfg.Sessions != nil {
		for _, sum := range h.cfg.Sessions() {
			h.Observe(sum)
		}
	}
}

// Enabled reports whether the hub is on.
func (h *Hub) Enabled() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

// Close stops the goroutine and, while enabled, writes a last line for every
// live session that changed since its previous one, so a daemon restart
// loses no totals. Safe on a hub that never started, and more than once.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.cancel()
	h.mu.Lock()
	started := h.started
	h.mu.Unlock()
	if started {
		<-h.done
	}
	h.mu.Lock()
	if h.enabled {
		h.flushLocked(true)
	}
	h.mu.Unlock()
	h.Sync()
}

// Sync writes every queued line before it returns.
func (h *Hub) Sync() {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	h.mu.Lock()
	q := h.queue
	h.queue = nil
	h.mu.Unlock()
	for _, r := range q {
		if err := h.cfg.Ledger.Append(r); err != nil {
			slog.Warn("usage: ledger append failed", "session", r.SessionID, "err", err)
		}
	}
}

func (h *Hub) run() {
	defer close(h.done)
	t := time.NewTicker(h.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.wake:
		case <-t.C:
			h.mu.Lock()
			if h.enabled {
				h.flushLocked(false)
			}
			h.mu.Unlock()
		}
		h.Sync()
	}
}

// terminal reports whether s will not run again unless resumed.
func terminal(s protocol.State) bool {
	return s == protocol.StateDone || s == protocol.StateFailed || s == protocol.StateCrashed
}

// Observe records the latest summary of a session (added or updated). A
// session that reached a terminal state gets its final ledger line queued
// now; one that runs again (a resume keeps its id) goes back to periodic
// lines and keeps counting under the same id. It never blocks on a file.
func (h *Hub) Observe(sum protocol.SessionSummary) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled || sum.ID == "" {
		return
	}
	_, known := h.live[sum.ID]
	l := h.liveLocked(sum.ID)
	l.sum = sum
	if !terminal(sum.State) {
		l.wroteFinal = false
		return
	}
	if l.wroteFinal {
		return
	}
	// First sight of an already finished session (daemon start, or the hub
	// was switched on): its ledger line may already be there.
	if p, ok := h.prior[sum.ID]; !known && ok && p.Ended && p.Tokens == sum.Tokens {
		l.wroteFinal = true
		return
	}
	h.enqueueLocked(l, true)
}

// Removed is called when a session leaves the board (user delete, retire
// reap, prune). Its last line is queued first if it has none yet, so the
// ledger keeps what the session used.
func (h *Hub) Removed(id string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.enabled {
		return
	}
	if l, ok := h.live[id]; ok {
		if !l.wroteFinal {
			h.enqueueLocked(l, true)
		}
		delete(h.live, id)
	}
}

// Report adds what the Claude tap said about a session: the conversation's
// cost so far (nil = unchanged) and the model in use ("" = unchanged). A
// session the hub has not seen is ignored.
func (h *Hub) Report(sessionID, conversationID string, costUSD *float64, modelID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.live[sessionID]
	if !h.enabled || !ok {
		return
	}
	h.reportLocked(l, conversationID, costUSD, modelID)
}

func (h *Hub) reportLocked(l *liveSession, conversationID string, costUSD *float64, modelID string) {
	if costUSD != nil {
		if l.costs == nil {
			l.costs = map[string]float64{}
		}
		if _, had := l.costs[conversationID]; had || len(l.costs) < maxConvs {
			l.costs[conversationID] = *costUSD
			l.hasCost = true
		}
	}
	if modelID != "" {
		l.model = modelID
	}
}

// liveLocked returns the session's entry, creating it on first sight. The
// first ledger line comes one FlushEvery later.
func (h *Hub) liveLocked(id string) *liveSession {
	if l, ok := h.live[id]; ok {
		return l
	}
	l := &liveSession{lastWrite: h.cfg.Now()}
	if p, ok := h.prior[id]; ok && p.CostUSD != nil {
		l.seedCost, l.hasCost = *p.CostUSD, true
	}
	h.live[id] = l
	return l
}

// recordLocked is the session's totals as a ledger record, stamped now.
func (h *Hub) recordLocked(l *liveSession, ended bool) Record {
	id := l.sum.ID
	r := Record{
		SessionID: id, Tool: l.sum.ToolID, Model: l.sum.ModelID,
		Tokens: l.sum.Tokens, Estimated: true, StartedAt: l.sum.StartedAt,
		At: h.cfg.Now().UTC(), Ended: ended,
	}
	if ended && !l.sum.EndedAt.IsZero() {
		r.At = l.sum.EndedAt.UTC()
	}
	if l.model != "" {
		r.Model = l.model
	}
	if p, ok := h.prior[id]; ok && p.Tokens > r.Tokens {
		r.Tokens = p.Tokens
	}
	if l.hasCost {
		sum := 0.0
		for _, c := range l.costs {
			sum += c
		}
		c := max(sum, l.seedCost)
		r.CostUSD = &c
	}
	return r
}

// same reports whether b adds nothing to a (a periodic line would repeat it).
func same(a, b Record) bool {
	if a.Tokens != b.Tokens || a.Model != b.Model || a.Ended != b.Ended || (a.CostUSD == nil) != (b.CostUSD == nil) {
		return false
	}
	return a.CostUSD == nil || *a.CostUSD == *b.CostUSD
}

// enqueueLocked queues l's current record and remembers it as written.
func (h *Hub) enqueueLocked(l *liveSession, final bool) {
	r := h.recordLocked(l, final)
	if r.Tool == "" || r.At.Before(h.cfg.Now().Add(-h.cfg.Retain)) {
		l.wroteFinal = l.wroteFinal || final
		return // never seen with a summary, or already past the retention
	}
	h.queue = append(h.queue, r)
	l.lastWrite, l.lastRec = h.cfg.Now(), r
	l.wroteFinal = final
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// flushLocked queues a periodic line for each live session that changed and
// whose last line is at least FlushEvery old; with force (shutdown) the age
// does not matter.
func (h *Hub) flushLocked(force bool) {
	now := h.cfg.Now()
	for _, l := range h.live {
		if l.wroteFinal || (!force && now.Sub(l.lastWrite) < h.cfg.FlushEvery) {
			continue
		}
		if !l.lastRec.At.IsZero() && same(l.lastRec, h.recordLocked(l, false)) {
			continue
		}
		h.enqueueLocked(l, false)
	}
}
