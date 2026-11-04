package lua

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// syncBuffer is a bytes.Buffer safe for slog writes from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func bufferLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// chanSender forwards rex.send calls to a channel without blocking.
func chanSender(ch chan<- sendCall) func(string, string) error {
	return func(id, text string) error {
		select {
		case ch <- sendCall{id, text}:
		default:
		}
		return nil
	}
}

// within runs fn and fails the test if it does not return within d.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %s", what, d)
	}
}

// P0-04: a runaway top-level init.lua must not hang daemon startup.
func TestLoadFile_TopLevelInfiniteLoopTimesOut(t *testing.T) {
	r, err := New(Options{
		Sender:      func(string, string) error { return nil },
		Lister:      defaultLister,
		LoadTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(r.Close)

	var loadErr error
	within(t, 2*time.Second, "LoadFile", func() {
		loadErr = r.LoadFile(writeTempLua(t, "while true do end"))
	})
	require.ErrorIs(t, loadErr, context.DeadlineExceeded)
}

// P0-04: a runaway handler is cut off at HandlerTimeout and the runtime
// keeps serving later events.
func TestOnEvent_HandlerInfiniteLoopTimesOut(t *testing.T) {
	sent := make(chan sendCall, 4)
	logger, logs := bufferLogger()
	r, err := New(Options{
		Logger:         logger,
		Sender:         chanSender(sent),
		Lister:         defaultLister,
		HandlerTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(r.Close)
	require.NoError(t, r.LoadFile(writeTempLua(t, `
rex.on("session_added", function(s) while true do end end)
rex.on("session_removed", function(s) rex.send(s.session_id, "after") end)
`)))

	within(t, 2*time.Second, "OnEvent", func() {
		require.NoError(t, r.OnEvent(protocol.EventSessionAdded, protocol.SessionSummary{ID: "a"}))
	})
	require.Contains(t, logs.String(), "lua: handler timed out")

	require.NoError(t, r.OnEvent(protocol.EventSessionRemoved, protocol.SessionRemoved{SessionID: "a"}))
	require.Equal(t, sendCall{"a", "after"}, <-sent)
}

// Close must interrupt a handler that is still running, not wait out its
// timeout, and the runtime must refuse calls afterwards.
func TestClose_InterruptsRunningHandler(t *testing.T) {
	sent := make(chan sendCall, 4)
	r, err := New(Options{
		Sender:         chanSender(sent),
		Lister:         defaultLister,
		HandlerTimeout: time.Hour,
	})
	require.NoError(t, err)
	require.NoError(t, r.LoadFile(writeTempLua(t, `
rex.on("session_added", function(s) rex.send(s.id, "started"); while true do end end)
`)))

	running := make(chan error, 1)
	go func() { running <- r.OnEvent(protocol.EventSessionAdded, protocol.SessionSummary{ID: "a"}) }()
	require.Equal(t, sendCall{"a", "started"}, <-sent)

	within(t, 2*time.Second, "Close", r.Close)
	require.NoError(t, <-running)
	require.ErrorIs(t, r.OnEvent(protocol.EventSessionAdded, protocol.SessionSummary{ID: "b"}), ErrClosed)
	require.ErrorIs(t, r.LoadFile(writeTempLua(t, "")), ErrClosed)
	r.Close() // idempotent
}
