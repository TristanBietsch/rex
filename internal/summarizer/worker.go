package summarizer

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

// TranscriptReader returns up to `max` bytes of sanitized transcript tail for
// the given session ID. Dependency-injected so unit tests don't touch disk.
type TranscriptReader func(sessionID string, max int) []byte

// Worker is a single-goroutine consumer of session-summary requests.
type Worker struct {
	cfg        Config
	store      *state.Store
	client     *Client
	transcript TranscriptReader

	ch chan string

	mu       sync.Mutex
	perSess  map[string]*sessionMeta
	failures atomic.Int32

	// availability: 1 = healthy, 0 = backend unavailable.
	available atomic.Int32
	onHealth  func(available bool, reason string) // optional callback set by daemon
}

type sessionMeta struct {
	lastSubmittedAt time.Time
	lastHash        uint64
}

// New builds a Worker. transcript is the function the worker uses to read the
// sanitized transcript tail from disk (usually state.TranscriptTail).
func New(cfg Config, store *state.Store, transcript TranscriptReader) *Worker {
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 2048
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
		perSess:    make(map[string]*sessionMeta),
	}
	w.available.Store(1)
	return w
}

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
		slog.Info("summarizer: backend_restored")
		if w.onHealth != nil {
			w.onHealth(true, "")
		}
	}
}

// Start launches the worker goroutine. Returns when ctx is canceled.
func (w *Worker) Start(ctx context.Context) error {
	slog.Info("summarizer: started", "model", w.cfg.Model, "base_url", w.cfg.BaseURL, "min_interval_ms", w.cfg.MinInterval.Milliseconds())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case id := <-w.ch:
			w.handle(ctx, id)
		}
	}
}

func (w *Worker) handle(ctx context.Context, id string) {
	if w.available.Load() == 0 {
		return
	}
	sess, ok := w.store.Get(id)
	if !ok {
		return
	}
	st := sess.State
	if st == protocol.StateDone || st == protocol.StateFailed || st == protocol.StateCrashed {
		return
	}

	w.mu.Lock()
	meta := w.perSess[id]
	if meta == nil {
		meta = &sessionMeta{}
		w.perSess[id] = meta
	}
	if time.Since(meta.lastSubmittedAt) < w.cfg.MinInterval {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	tail := w.transcript(id, w.cfg.MaxBytes)
	prompt := buildPrompt(sess.ToolID, sess.Slug, string(tail))

	h := fnv.New64a()
	_, _ = h.Write([]byte(prompt))
	hash := h.Sum64()

	w.mu.Lock()
	if hash == meta.lastHash && meta.lastHash != 0 {
		w.mu.Unlock()
		slog.Debug("summarizer: skipped_unchanged", "session", id)
		return
	}
	// Updated before the call: a failed call still consumes MinInterval so a
	// degraded backend doesn't get hammered per supervisor tick.
	meta.lastSubmittedAt = time.Now()
	w.mu.Unlock()

	bytesIn := len(tail)
	slog.Debug("summarizer: request", "session", id, "bytes_in", bytesIn)
	start := time.Now()

	// Single budget for both attempts; if the first call burns RequestTimeout,
	// the retry may not have headroom — that's acceptable (it'll see ctx.Done()).
	callCtx, cancel := context.WithTimeout(ctx, w.cfg.RequestTimeout+500*time.Millisecond)
	defer cancel()

	resp, err := w.callWithRetry(callCtx, prompt)
	elapsed := time.Since(start)
	if err != nil {
		w.failures.Add(1)
		slog.Warn("summarizer: error", "session", id, "err", err)
		if w.failures.Load() >= 3 {
			w.MarkUnavailable("consecutive call failures")
		}
		return
	}
	w.failures.Store(0)
	if elapsed > 2*time.Second {
		slog.Info("summarizer: slow_call", "session", id, "duration_ms", elapsed.Milliseconds())
	}

	cleaned := cleanResponse(resp)
	slog.Debug("summarizer: response", "session", id, "duration_ms", elapsed.Milliseconds(), "chars_out", len([]rune(cleaned)))
	if cleaned == "" {
		return
	}

	w.mu.Lock()
	meta.lastHash = hash
	w.mu.Unlock()

	if err := w.store.UpdateDescription(id, cleaned); err != nil {
		slog.Warn("summarizer: update_description failed", "session", id, "err", err)
	}
}

func (w *Worker) callWithRetry(ctx context.Context, prompt string) (string, error) {
	resp, err := w.client.Generate(ctx, prompt)
	if err == nil {
		return resp, nil
	}
	select {
	case <-ctx.Done():
		return "", err
	case <-time.After(500 * time.Millisecond):
	}
	return w.client.Generate(ctx, prompt)
}
