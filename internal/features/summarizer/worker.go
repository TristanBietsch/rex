package summarizer

import (
	"bytes"
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TranscriptReader returns up to `max` bytes of sanitized transcript tail for
// the given session ID. Dependency-injected so unit tests don't touch disk.
type TranscriptReader func(sessionID string, max int) []byte

// Worker is a single-goroutine consumer of session-summary requests. Requests
// arriving on Channel are coalesced per session and scheduled oldest-first;
// see schedule.go.
type Worker struct {
	cfg        Config
	store      *state.Store
	client     *Client
	transcript TranscriptReader

	subscribed chan struct{} // closed by Start once it is subscribed to the store (tests wait on it)
	retryDelay time.Duration // pause before the one transient retry
	detail     atomic.Int32  // a Detail; the zero value is DetailMedium

	cadence Cadence
	now     func() time.Time // the clock of anchors and ticks; time.Now unless a test replaces it
	schedOn atomic.Bool      // summary_schedule: ticks run and output-driven sends are ignored

	ch   chan string
	wake chan struct{} // capacity 1: nudges the scheduler loop
	slot chan struct{} // capacity 1: the shared Ollama slot
	// pending, perSess and sched are guarded by mu.
	mu       sync.Mutex
	pending  map[string]pendingReq
	arrivals uint64 // counts new pending entries: pendingReq.seq
	perSess  map[string]*sessionMeta
	sched    map[string]*schedEntry
	failures atomic.Int32

	// availability: 1 = healthy, 0 = backend unavailable.
	available atomic.Int32
	onHealth  func(available bool, reason string) // optional callback set by daemon
}

type sessionMeta struct {
	lastSubmittedAt time.Time
	lastHash        uint64
	// final is set once a terminal-state (done, failed, crashed) summary has
	// been produced; it is cleared when the session goes live again.
	final bool
	// callStart and callEnd bound the session's latest handle; a tick due
	// inside that window merges with the call instead of making another.
	callStart, callEnd time.Time
	inflight           bool // a call for the session is running
	// merged is set when a tick came due during the running call and was
	// merged into it; mergedLate is that tick's phase.
	merged, mergedLate bool
}

func isTerminal(s protocol.State) bool {
	return s == protocol.StateDone || s == protocol.StateFailed || s == protocol.StateCrashed
}

// preferredFallbacks are small, fast, non-"thinking" models suited to a
// one-line summary. A pulled model outside this list is never picked: big
// models (llama3.1 8B, qwen3 30B) blow the request timeout on cold load.
var preferredFallbacks = []string{
	"gemma2:2b", "llama3.2:3b", "llama3.2:latest", "llama3.2:1b",
	"gemma3:1b", "gemma3:4b", "qwen2.5:3b", "qwen2.5:1.5b", "phi3:mini",
}

func ResolveModel(configured string, pulled []string) (string, bool) {
	has := func(name string) (string, bool) {
		for _, p := range pulled {
			// Ollama lists untagged pulls as "name:latest".
			if p == name || p == name+":latest" {
				return p, true
			}
		}
		return "", false
	}
	if configured != "" {
		if p, ok := has(configured); ok {
			return p, true
		}
	}
	for _, pref := range preferredFallbacks {
		if p, ok := has(pref); ok {
			return p, true
		}
	}
	return "", false
}

// New builds a Worker. transcript is the function the worker uses to read the
// sanitized transcript tail from disk (usually state.TranscriptTail).
func New(cfg Config, store *state.Store, transcript TranscriptReader) *Worker {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 2048
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	if cfg.MinInterval == 0 {
		cfg.MinInterval = 800 * time.Millisecond
	}
	w := &Worker{
		cfg:        cfg,
		store:      store,
		client:     NewClient(cfg),
		transcript: transcript,
		ch:         make(chan string, 64),
		subscribed: make(chan struct{}),
		retryDelay: 500 * time.Millisecond,
		wake:       make(chan struct{}, 1),
		slot:       make(chan struct{}, 1),
		pending:    make(map[string]pendingReq),
		perSess:    make(map[string]*sessionMeta),
		sched:      make(map[string]*schedEntry),
		cadence:    cfg.Cadence.orDefault(),
		now:        time.Now,
	}
	w.schedOn.Store(cfg.Schedule)
	w.available.Store(1)
	w.detail.Store(int32(cfg.Detail.valid()))
	return w
}

// Detail returns the current description detail level.
func (w *Worker) Detail() Detail { return Detail(w.detail.Load()) }

// SetDetail changes the description detail level for later requests. Safe
// from any goroutine, including mid-call: a request in flight keeps the level
// it was built with. The prompt (and so the unchanged-prompt hash) changes
// with the level, so existing sessions are re-summarized on their next
// request.
func (w *Worker) SetDetail(d Detail) { w.detail.Store(int32(d.valid())) }

// Channel returns the send-side of the request channel.
func (w *Worker) Channel() chan<- string { return w.ch }

// SetHealthCallback installs a callback invoked whenever backend availability flips.
// Call it once before Start; it is not safe to call concurrently with Start,
// MarkUnavailable, or MarkAvailable.
func (w *Worker) SetHealthCallback(fn func(available bool, reason string)) {
	w.onHealth = fn
}

// BackendAvailable reports whether the worker currently believes Ollama is reachable.
func (w *Worker) BackendAvailable() bool { return w.available.Load() == 1 }

// MarkUnavailable flips the flag to false.
func (w *Worker) MarkUnavailable(reason string) {
	if w.available.Swap(0) == 1 {
		slog.Warn("summarizer: backend_unavailable", "reason", reason)
		if w.onHealth != nil {
			w.onHealth(false, reason)
		}
	}
}

// MarkAvailable flips the flag to true.
func (w *Worker) MarkAvailable() {
	if w.available.Swap(1) == 0 {
		w.failures.Store(0)
		slog.Info("summarizer: backend_restored")
		w.wakeup() // requests kept while down run now
		if w.onHealth != nil {
			w.onHealth(true, "")
		}
	}
}

// SetModel switches the model subsequent Generate calls use. Intended for
// the daemon's health probe after substituting a fallback when the
// configured model isn't pulled. Safe to call from any goroutine, including
// while the worker is mid-call: the client holds the name atomically, and
// w.cfg stays the immutable construction-time config (cfg.Model is the
// configured model, Model() the live one).
func (w *Worker) SetModel(model string) {
	w.client.SetModel(model)
}

// Model returns the model the worker currently sends requests to.
func (w *Worker) Model() string { return w.client.Model() }

// Start launches the worker goroutine. Returns when ctx is canceled.
func (w *Worker) Start(ctx context.Context) error {
	slog.Info("summarizer: started", "model", w.Model(), "base_url", w.cfg.BaseURL, "min_interval_ms", w.cfg.MinInterval.Milliseconds())
	// The callback follows the store contract: it takes a mutex and returns.
	unsub := w.store.Subscribe(w.onEvent)
	defer unsub()
	w.seedSchedule()
	close(w.subscribed)
	return w.loop(ctx)
}

// onEvent reacts to the store: a removed session is forgotten; a state change
// queues the session ahead of ordinary requests, because the state changes
// what the description should say. A move to done, failed or crashed queues
// the one final summary; a move back to a live state (a resume, or undoing
// done) re-arms it. It runs on the store's goroutines, so it only takes a
// mutex and returns.
func (w *Worker) onEvent(e state.Event) {
	w.trackEvent(e)
	switch {
	case e.Kind == state.EventRemoved:
		w.forget(e.SessionID)
	case e.Kind == state.EventReopened:
		// A new run: its final summary is due again (the live-state change
		// that follows would also say so, but a failure can come first).
		w.mu.Lock()
		if m := w.perSess[e.SessionID]; m != nil {
			m.final = false
		}
		w.mu.Unlock()
	case e.Kind == state.EventUpdated && e.NewState != nil:
		if !isTerminal(*e.NewState) {
			w.mu.Lock()
			if m := w.perSess[e.SessionID]; m != nil {
				m.final = false
			}
			w.mu.Unlock()
		}
		w.enqueue(e.SessionID, time.Now(), true)
		w.wakeup()
	}
}

func (w *Worker) handle(ctx context.Context, id string, req pendingReq) {
	sess, ok := w.store.Get(id)
	if !ok {
		return
	}
	snap := sess.Summary()
	terminal := isTerminal(snap.State)

	w.mu.Lock()
	meta := w.perSess[id]
	if meta == nil {
		meta = &sessionMeta{}
		w.perSess[id] = meta
	}
	if terminal && meta.final {
		// One final summary per run of the session, then nothing more.
		w.mu.Unlock()
		return
	}
	meta.callStart, meta.inflight, meta.merged = w.now(), true, false
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		meta.callEnd, meta.inflight = w.now(), false
		w.mu.Unlock()
	}()

	tail := w.transcript(id, w.cfg.MaxBytes)
	if len(bytes.TrimSpace(tail)) == 0 {
		// Nothing readable yet: a prompt with only the slug makes the model
		// invent an activity. The board shows the title meanwhile.
		slog.Debug("summarizer: skipped_empty_screen", "session", id)
		return
	}
	task := snap.Title
	if task == "" {
		task = snap.Slug
	}
	kind, detail := KindActivity, w.Detail()
	if req.late && !req.prio && !terminal && w.schedOn.Load() {
		// Late phase of the scheduled refresh: one level more detail, and
		// what is finished plus what is happening now.
		kind, detail = KindProgress, detail.Up()
	}
	prompt, err := BuildPrompt(kind, Input{Tool: snap.ToolID, Task: task, State: snap.State, Text: string(tail)}, detail)
	if err != nil {
		slog.Warn("summarizer: build prompt failed", "session", id, "err", err)
		return
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(prompt.System))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(prompt.User))
	hash := h.Sum64()

	w.mu.Lock()
	if hash == meta.lastHash && meta.lastHash != 0 && !req.prio {
		if terminal {
			meta.final = true
		}
		w.mu.Unlock()
		slog.Debug("summarizer: skipped_unchanged", "session", id)
		return
	}
	// Updated before the call: a failed call still consumes MinInterval so a
	// degraded backend doesn't get hammered.
	meta.lastSubmittedAt = time.Now()
	w.mu.Unlock()

	bytesIn := len(tail)
	w.mu.Lock()
	queueLen := len(w.pending)
	w.mu.Unlock()
	slog.Debug("summarizer: request", "session", id, "bytes_in", bytesIn,
		"wait_ms", time.Since(req.since).Milliseconds(), "queue_len", queueLen, "model", w.Model())
	start := time.Now()

	resp, err := w.complete(ctx, prompt)
	elapsed := time.Since(start)
	if err != nil {
		w.failures.Add(1)
		slog.Warn("summarizer: error", "session", id, "err", err)
		if ctx.Err() == nil {
			// Deferred, not dropped: runs again after MinInterval, or once
			// the backend is back if these failures marked it unavailable.
			w.enqueueReq(id, pendingReq{since: time.Now(), prio: req.prio, late: req.late}) // since=now: a failing session does not keep jumping the queue
		}
		if w.failures.Load() >= 3 {
			w.MarkUnavailable("consecutive call failures")
		}
		return
	}
	w.failures.Store(0)
	if elapsed > 2*time.Second {
		slog.Info("summarizer: slow_call", "session", id, "duration_ms", elapsed.Milliseconds())
	}

	cleaned := Clean(resp, prompt.MaxRunes, snap.ToolID)
	slog.Debug("summarizer: response", "session", id, "duration_ms", elapsed.Milliseconds(), "chars_out", len([]rune(cleaned)))
	if cleaned == "" {
		// A tick merged into this call got nothing from it: ask once more.
		w.mu.Lock()
		if meta.merged {
			w.mergeLocked(id, pendingReq{since: time.Now(), late: meta.mergedLate})
			meta.merged = false
		}
		w.mu.Unlock()
		return
	}

	w.mu.Lock()
	meta.lastHash = hash
	if terminal {
		meta.final = true
	}
	w.mu.Unlock()

	if err := w.store.UpdateDescription(id, cleaned); err != nil {
		slog.Warn("summarizer: update_description failed", "session", id, "err", err)
	}
}

// callWithRetry makes one attempt, and a second after retryDelay only when
// the first failed transiently and ctx is still live. Each attempt has its
// own RequestTimeout, and holds the Ollama slot only while it runs.
func (w *Worker) callWithRetry(ctx context.Context, cr CompleteRequest) (string, error) {
	resp, err := w.attempt(ctx, cr)
	if err == nil || !transient(err) || ctx.Err() != nil {
		return resp, err
	}
	select {
	case <-ctx.Done():
		return "", err
	case <-time.After(w.retryDelay):
	}
	return w.attempt(ctx, cr)
}

func (w *Worker) attempt(ctx context.Context, cr CompleteRequest) (string, error) {
	release, err := w.acquireSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	actx, cancel := context.WithTimeout(ctx, w.cfg.RequestTimeout)
	defer cancel()
	return w.client.Complete(actx, cr)
}
