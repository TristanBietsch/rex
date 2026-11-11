package summarizer

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestBuildPrompt_UnknownKindErrors(t *testing.T) {
	_, err := BuildPrompt(Kind("nope"), Input{}, DetailMedium)
	require.Error(t, err)
}

func TestDetail_Caps(t *testing.T) {
	for _, tc := range []struct {
		d               Detail
		name            string
		maxRunes, token int
	}{
		{DetailShort, "short", 60, 40},
		{DetailMedium, "medium", 110, 64},
		{DetailLong, "long", 180, 100},
		{Detail(99), "medium", 110, 64},
		{Detail(-1), "medium", 110, 64},
	} {
		require.Equal(t, tc.name, tc.d.String())
		require.Equal(t, tc.maxRunes, tc.d.MaxRunes(), tc.name)
		require.Equal(t, tc.token, tc.d.NumPredict(), tc.name)
	}
}

func TestSummarize_ReturnsCleanedLine(t *testing.T) {
	f := newFakeOllama(t, nil)
	w := New(Config{BaseURL: f.srv.URL, Model: "m"}, state.NewStore(), uniqueTranscript(nil, nil))
	got, err := w.Summarize(context.Background(), KindActivity,
		Input{Tool: "codex", Task: "t", State: protocol.StateWorking, Text: "running tests"})
	require.NoError(t, err)
	require.Equal(t, "Running tests", got)
}

func TestSummarize_ErrUnavailable(t *testing.T) {
	f := newFakeOllama(t, nil)
	w := New(Config{BaseURL: f.srv.URL, Model: "m"}, state.NewStore(), uniqueTranscript(nil, nil))
	w.MarkUnavailable("test")
	_, err := w.Summarize(context.Background(), KindActivity, Input{Text: "x"})
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, 0, f.count())
}

func TestSummarize_UnknownKind(t *testing.T) {
	w := New(Config{Model: "m"}, state.NewStore(), uniqueTranscript(nil, nil))
	_, err := w.Summarize(context.Background(), Kind("nope"), Input{})
	require.Error(t, err)
}

// A concurrent Summarize and the worker loop share one Ollama slot: the fake
// counts requests in flight and must never see two.
func TestSummarize_UsesSharedSlot(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	f := newFakeOllama(t, func(_ int, _ http.ResponseWriter) bool {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		inFlight.Add(-1)
		return false
	})
	st, _ := newTestStoreWithSession("sess-slot")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-slot"
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := w.Summarize(context.Background(), KindActivity, Input{Text: "screen"})
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Eventually(t, func() bool { return f.count() >= 4 }, 3*time.Second, 5*time.Millisecond)
	require.Equal(t, int32(1), maxInFlight.Load(), "loop and Summarize overlapped")
}
