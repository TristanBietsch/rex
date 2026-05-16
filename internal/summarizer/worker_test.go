package summarizer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

func newTestStoreWithSession(id string) (*state.Store, *state.Session) {
	st := state.NewStore()
	sess := &state.Session{
		ID:        id,
		ShortID:   id[:4],
		ToolID:    "codex",
		Slug:      "test-task",
		State:     protocol.StateWorking,
		StartedAt: time.Now().UTC(),
		LastLine:  "running pnpm test:billing",
	}
	_ = st.Add(sess)
	return st, sess
}

func TestWorkerCallsOllamaAndWritesDescription(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"running pnpm test"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-aaaa")

	got := make(chan string, 1)
	st.Subscribe(func(e state.Event) {
		if v, ok := e.Patch["description"].(string); ok {
			select {
			case got <- v:
			default:
			}
		}
	})

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("recent transcript bytes"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	w.Channel() <- "sess-aaaa"

	select {
	case desc := <-got:
		if desc != "running pnpm test" {
			t.Fatalf("desc: %q", desc)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no description delivered")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestWorkerSkipsIfUnchangedHash(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"running tests"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-bbbb")

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("identical bytes"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-bbbb"
	time.Sleep(150 * time.Millisecond)
	w.Channel() <- "sess-bbbb" // second signal with identical transcript → skip
	time.Sleep(150 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 Ollama call (skip-if-unchanged), got %d", got)
	}
}

func TestWorkerSkipsTerminalState(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	st, sess := newTestStoreWithSession("sess-cccc")
	sess.State = protocol.StateDone

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 0
	w := New(cfg, st, transcriptStub("anything"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-cccc"
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected 0 calls (terminal state), got %d", got)
	}
}

func TestWorkerMinIntervalGate(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.WriteString(w, `{"response":"x"}`)
	}))
	defer srv.Close()
	st, _ := newTestStoreWithSession("sess-dddd")

	cfg := Defaults()
	cfg.BaseURL = srv.URL
	cfg.MinInterval = 500 * time.Millisecond
	// Vary transcript per call so skip-if-unchanged doesn't dominate.
	var i int32
	w := New(cfg, st, func(_ string, _ int) []byte {
		v := atomic.AddInt32(&i, 1)
		return []byte("bytes-" + string(rune('0'+v)))
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	w.Channel() <- "sess-dddd"
	time.Sleep(100 * time.Millisecond)
	w.Channel() <- "sess-dddd" // dropped — within MinInterval
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call (min-interval gate), got %d", got)
	}
}

// transcriptStub is the TranscriptReader fixture used by the tests: returns the
// same bytes regardless of session/max.
func transcriptStub(s string) TranscriptReader {
	return func(_ string, _ int) []byte { return []byte(s) }
}
