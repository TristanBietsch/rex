package summarizer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeOllama is an httptest Ollama that counts and timestamps requests. Its
// optional hook runs inside the handler (to block or fail a request); n is
// the 1-based request number.
type fakeOllama struct {
	srv   *httptest.Server
	mu    sync.Mutex
	times []time.Time
	calls atomic.Int32
}

func newFakeOllama(t *testing.T, hook func(n int, w http.ResponseWriter) (handled bool)) *fakeOllama {
	t.Helper()
	f := &fakeOllama{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := int(f.calls.Add(1))
		f.mu.Lock()
		f.times = append(f.times, time.Now())
		f.mu.Unlock()
		if hook != nil && hook(n, w) {
			return
		}
		_, _ = io.WriteString(w, `{"response":"running tests"}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOllama) count() int { return int(f.calls.Load()) }

// uniqueTranscript returns a TranscriptReader whose text differs on every
// read, so skip-if-unchanged never hides a scheduling bug. order records the
// session ids in the order the worker reads them.
func uniqueTranscript(order *[]string, mu *sync.Mutex) TranscriptReader {
	var n atomic.Int32
	return func(id string, _ int) []byte {
		if order != nil {
			mu.Lock()
			*order = append(*order, id)
			mu.Unlock()
		}
		return fmt.Appendf(nil, "screen text %d", n.Add(1))
	}
}

func addSession(t *testing.T, st *state.Store, id string) {
	t.Helper()
	require.NoError(t, st.Add(&state.Session{
		ID: id, ShortID: id[:4], ToolID: "codex", Slug: "task-" + id,
		State: protocol.StateWorking, StartedAt: time.Now().UTC(),
	}))
}

func startWorker(t *testing.T, cfg Config, st *state.Store, tr TranscriptReader) *Worker {
	t.Helper()
	w := New(cfg, st, tr)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Start(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w
}

func TestWorker_DefersInsteadOfDropping(t *testing.T) {
	f := newFakeOllama(t, nil)
	st, _ := newTestStoreWithSession("sess-defer")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 300 * time.Millisecond
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-defer"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	w.Channel() <- "sess-defer" // inside MinInterval: deferred, not dropped
	require.Eventually(t, func() bool { return f.count() == 2 }, 3*time.Second, 5*time.Millisecond)

	f.mu.Lock()
	gap := f.times[1].Sub(f.times[0])
	f.mu.Unlock()
	require.GreaterOrEqual(t, gap, 290*time.Millisecond, "second call must wait out MinInterval")
}

func TestWorker_CoalescesDuplicates(t *testing.T) {
	release := make(chan struct{})
	f := newFakeOllama(t, func(n int, _ http.ResponseWriter) bool {
		if n == 1 {
			<-release
		}
		return false
	})
	st, _ := newTestStoreWithSession("sess-coal")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 0
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-coal"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	for range 10 {
		w.Channel() <- "sess-coal" // all while the first call is blocked
	}
	close(release)
	require.Eventually(t, func() bool { return f.count() == 2 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 2, f.count(), "ten duplicate sends collapse into one more call")
}

func TestWorker_OldestFirstAcrossSessions(t *testing.T) {
	release := make(chan struct{})
	f := newFakeOllama(t, func(n int, _ http.ResponseWriter) bool {
		if n == 1 {
			<-release
		}
		return false
	})
	st := state.NewStore()
	for _, id := range []string{"sess-aaaa", "sess-bbbb", "sess-cccc"} {
		addSession(t, st, id)
	}
	var (
		mu    sync.Mutex
		order []string
	)
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 0
	w := startWorker(t, cfg, st, uniqueTranscript(&order, &mu))

	w.Channel() <- "sess-aaaa"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	w.Channel() <- "sess-cccc"
	time.Sleep(20 * time.Millisecond)
	w.Channel() <- "sess-bbbb"
	time.Sleep(20 * time.Millisecond)
	w.Channel() <- "sess-cccc" // a duplicate must not jump or lose its place
	close(release)
	require.Eventually(t, func() bool { return f.count() == 3 }, 2*time.Second, 5*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"sess-aaaa", "sess-cccc", "sess-bbbb"}, order, "oldest request first")
}

func TestWorker_PendingSurvivesUnavailable(t *testing.T) {
	f := newFakeOllama(t, nil)
	st, _ := newTestStoreWithSession("sess-down")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 0
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.MarkUnavailable("test outage")
	w.Channel() <- "sess-down"
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 0, f.count(), "no calls while the backend is down")
	w.MarkAvailable()
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond,
		"the kept request runs when the backend returns")
}

func TestWorker_RequeuesAfterFailedCall(t *testing.T) {
	// One attempt is the call plus its retry: fail the first two requests.
	f := newFakeOllama(t, func(n int, w http.ResponseWriter) bool {
		if n <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return true
		}
		return false
	})
	st, _ := newTestStoreWithSession("sess-fail")
	got := make(chan string, 4)
	st.Subscribe(func(e state.Event) {
		if v, ok := e.Patch["description"].(string); ok {
			got <- v
		}
	})
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 50 * time.Millisecond
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-fail"
	select {
	case v := <-got:
		require.Equal(t, "Running tests", v)
	case <-time.After(5 * time.Second):
		t.Fatal("failed request was dropped, not retried later")
	}
}

func TestWorker_ForgetsRemovedSession(t *testing.T) {
	f := newFakeOllama(t, nil)
	st, _ := newTestStoreWithSession("sess-gone")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 0
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-gone"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	held := func() (int, int) {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.perSess), len(w.pending)
	}
	require.Eventually(t, func() bool { p, _ := held(); return p == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, st.Remove("sess-gone"))
	require.Eventually(t, func() bool { p, q := held(); return p == 0 && q == 0 }, time.Second, 5*time.Millisecond)
}

func TestWorker_SharedSlotSerializesCalls(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	f := newFakeOllama(t, func(_ int, _ http.ResponseWriter) bool {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inFlight.Add(-1)
		return false
	})
	st := state.NewStore()
	w := New(Config{BaseURL: f.srv.URL, Model: "m", MinInterval: 0}, st, uniqueTranscript(nil, nil))
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.callWithRetry(context.Background(), "p")
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), maxInFlight.Load(), "the Ollama slot admits one call at a time")
}
