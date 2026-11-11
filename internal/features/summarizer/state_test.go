package summarizer

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestWorker_StateChangeEnqueuesPriority(t *testing.T) {
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
	w := startWorker(t, cfg, st, uniqueTranscript(&order, &mu))

	w.Channel() <- "sess-aaaa"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	w.Channel() <- "sess-bbbb" // older, ordinary
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, st.Transition("sess-cccc", protocol.StateNeedsInput)) // newer, but a state change
	close(release)
	require.Eventually(t, func() bool { return f.count() == 3 }, 3*time.Second, 5*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"sess-aaaa", "sess-cccc", "sess-bbbb"}, order, "the state change jumps the queue")
}

// A state change runs even when the prompt is byte-identical to the last one
// (queued and working read the same to the model); an ordinary request does not.
func TestWorker_StateChangeBypassesUnchangedHash(t *testing.T) {
	f := newFakeOllama(t, nil)
	st := state.NewStore()
	require.NoError(t, st.Add(&state.Session{ID: "sess-hash", ShortID: "sess", ToolID: "codex", Slug: "s",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}))
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 20 * time.Millisecond
	w := startWorker(t, cfg, st, transcriptStub("same screen"))

	w.Channel() <- "sess-hash"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	w.Channel() <- "sess-hash"
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 1, f.count(), "ordinary request with an unchanged prompt is skipped")

	require.NoError(t, st.Transition("sess-hash", protocol.StateWorking))
	require.Eventually(t, func() bool { return f.count() == 2 }, 2*time.Second, 5*time.Millisecond)
}

func TestWorker_FinalSummaryOnceAtDone(t *testing.T) {
	f := newFakeOllama(t, nil)
	st, _ := newTestStoreWithSession("sess-done")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 20 * time.Millisecond
	w := startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	w.Channel() <- "sess-done"
	require.Eventually(t, func() bool { return f.count() == 1 }, 2*time.Second, 5*time.Millisecond)

	require.NoError(t, st.Transition("sess-done", protocol.StateDone))
	require.Eventually(t, func() bool { return f.count() == 2 }, 2*time.Second, 5*time.Millisecond, "final summary at done")
	w.Channel() <- "sess-done"
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, 2, f.count(), "no summary after the final one")

	// The session goes live again (resume, or undo done): the next done gets
	// its own final summary.
	require.NoError(t, st.Transition("sess-done", protocol.StateWorking))
	require.Eventually(t, func() bool { return f.count() == 3 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, st.Transition("sess-done", protocol.StateFailed))
	require.Eventually(t, func() bool { return f.count() == 4 }, 2*time.Second, 5*time.Millisecond, "final summary again after a reopen")
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 4, f.count())
}

func TestWorker_FinalSummaryFailureIsRetried(t *testing.T) {
	// One attempt = call + transient retry; fail the first attempt only.
	f := newFakeOllama(t, func(n int, w http.ResponseWriter) bool {
		if n <= 2 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return true
		}
		return false
	})
	st, _ := newTestStoreWithSession("sess-retry")
	cfg := Defaults()
	cfg.BaseURL = f.srv.URL
	cfg.MinInterval = 30 * time.Millisecond
	startWorker(t, cfg, st, uniqueTranscript(nil, nil))

	require.NoError(t, st.Transition("sess-retry", protocol.StateDone))
	require.Eventually(t, func() bool { return f.count() == 3 }, 5*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return st.Screen("sess-retry") == "" && descriptionOf(st, "sess-retry") != "" },
		2*time.Second, 5*time.Millisecond, "the final description is stored after the retry")
}

func descriptionOf(st *state.Store, id string) string {
	s, ok := st.Get(id)
	if !ok {
		return ""
	}
	return s.Summary().Description
}
