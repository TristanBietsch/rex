package summarizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
)

func retryWorker(t *testing.T, f *fakeOllama, timeout time.Duration) *Worker {
	t.Helper()
	w := New(Config{BaseURL: f.srv.URL, Model: "m", RequestTimeout: timeout}, state.NewStore(), uniqueTranscript(nil, nil))
	w.retryDelay = 10 * time.Millisecond
	return w
}

func TestWorker_NoRetryAfterDeadline(t *testing.T) {
	f := newFakeOllama(t, func(_ int, _ http.ResponseWriter) bool {
		time.Sleep(300 * time.Millisecond) // longer than RequestTimeout
		return false
	})
	w := retryWorker(t, f, 60*time.Millisecond)

	_, err := w.callWithRetry(context.Background(), CompleteRequest{Prompt: "p"})
	require.Error(t, err)
	time.Sleep(400 * time.Millisecond)
	require.Equal(t, 1, f.count(), "a timed-out attempt is never retried")
}

func TestWorker_RetriesTransient500(t *testing.T) {
	f := newFakeOllama(t, func(n int, w http.ResponseWriter) bool {
		if n == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	w := retryWorker(t, f, time.Second)

	got, err := w.callWithRetry(context.Background(), CompleteRequest{Prompt: "p"})
	require.NoError(t, err)
	require.Equal(t, "running tests", got)
	require.Equal(t, 2, f.count())
}

func TestWorker_NoRetryOnClientError(t *testing.T) {
	f := newFakeOllama(t, func(_ int, w http.ResponseWriter) bool {
		http.Error(w, "model not found", http.StatusNotFound)
		return true
	})
	w := retryWorker(t, f, time.Second)

	_, err := w.callWithRetry(context.Background(), CompleteRequest{Prompt: "p"})
	require.Error(t, err)
	require.Equal(t, 1, f.count(), "a 4xx will not get better on retry")
}

// Each attempt gets its own RequestTimeout: a slow first attempt that still
// fails transiently does not starve the retry.
func TestWorker_RetryGetsFullTimeout(t *testing.T) {
	f := newFakeOllama(t, func(n int, w http.ResponseWriter) bool {
		if n == 1 {
			time.Sleep(40 * time.Millisecond)
			http.Error(w, "busy", http.StatusBadGateway)
			return true
		}
		time.Sleep(40 * time.Millisecond) // within a fresh 80ms budget
		return false
	})
	w := retryWorker(t, f, 80*time.Millisecond)

	_, err := w.callWithRetry(context.Background(), CompleteRequest{Prompt: "p"})
	require.NoError(t, err)
	require.Equal(t, 2, f.count())
}

func TestTransient(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline", context.DeadlineExceeded, false},
		{"canceled", context.Canceled, false},
		{"wrapped deadline", fmt.Errorf("Post: %w", context.DeadlineExceeded), false},
		{"500", &statusError{Code: 500}, true},
		{"503", fmt.Errorf("x: %w", &statusError{Code: 503}), true},
		{"404", &statusError{Code: 404}, false},
		{"refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"reset", fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{"eof", io.ErrUnexpectedEOF, true},
		{"decode", errors.New("decode: bad json"), false},
	} {
		require.Equal(t, tc.want, transient(tc.err), tc.name)
	}
}
