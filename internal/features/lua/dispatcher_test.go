package lua

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// gatedHandler records delivered events. The first call blocks until
// release is closed (or the dispatcher cancels it), which keeps later events
// queued so tests can shape the queue deterministically.
type gatedHandler struct {
	started chan struct{} // closed when the first call begins
	release chan struct{}

	mu        sync.Mutex
	got       []string
	first     sync.Once
	delivered chan struct{} // one value per completed call
}

func newGatedHandler() *gatedHandler {
	return &gatedHandler{
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		delivered: make(chan struct{}, 1024),
	}
}

func (h *gatedHandler) OnEventContext(ctx context.Context, eventType string, data any) error {
	isFirst := false
	h.first.Do(func() { isFirst = true })
	if isFirst {
		close(h.started)
		select {
		case <-h.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	h.mu.Lock()
	h.got = append(h.got, describe(eventType, data))
	h.mu.Unlock()
	h.delivered <- struct{}{}
	return nil
}

func (h *gatedHandler) waitDelivered(t *testing.T, n int) []string {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-h.delivered:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d events delivered", i, n)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.got...)
}

func describe(eventType string, data any) string {
	switch v := data.(type) {
	case protocol.SessionSummary:
		return fmt.Sprintf("%s:%s:%s", normaliseEventType(eventType), v.ID, v.Title)
	case protocol.SessionRemoved:
		return fmt.Sprintf("%s:%s", normaliseEventType(eventType), v.SessionID)
	default:
		return normaliseEventType(eventType)
	}
}

func newTestDispatcher(t *testing.T, h EventHandler, size int) (*Dispatcher, *syncBuffer) {
	t.Helper()
	logger, logs := bufferLogger()
	d := NewDispatcher(h, DispatcherOptions{QueueSize: size, Logger: logger})
	t.Cleanup(d.Close)
	return d, logs
}

func summary(id, title string) protocol.SessionSummary {
	return protocol.SessionSummary{ID: id, Title: title}
}

// blockFirst enqueues an event and waits until the handler is parked on it,
// so everything enqueued afterwards stays queued until h.release closes.
func blockFirst(t *testing.T, d *Dispatcher, h *gatedHandler) {
	t.Helper()
	require.True(t, d.Enqueue(protocol.EventSessionAdded, "s0", summary("s0", "")))
	select {
	case <-h.started:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher never delivered the first event")
	}
}

// P0-04: Enqueue runs on the store's broadcast path (often the PTY reader)
// and must never wait for a handler.
func TestDispatcher_EnqueueDoesNotWaitForHandler(t *testing.T) {
	h := newGatedHandler()
	d, _ := newTestDispatcher(t, h, 64)
	within(t, time.Second, "Enqueue while the handler is blocked", func() {
		blockFirst(t, d, h)
		for i := 0; i < 10; i++ {
			d.Enqueue(protocol.EventSessionRemoved, fmt.Sprint("r", i), protocol.SessionRemoved{SessionID: fmt.Sprint("r", i)})
		}
	})
	close(h.release)
	require.Len(t, h.waitDelivered(t, 11), 11)
}

// session_updated for a session that is still queued replaces the queued
// payload in place: only the latest state is delivered, in the slot of the
// first update.
func TestDispatcher_CoalescesQueuedSessionUpdates(t *testing.T) {
	h := newGatedHandler()
	d, _ := newTestDispatcher(t, h, 64)
	blockFirst(t, d, h)

	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s1", summary("s1", "v1")))
	require.True(t, d.Enqueue("session_updated", "s2", summary("s2", "v1")))
	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s1", summary("s1", "v2")))
	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s1", summary("s1", "v3")))
	close(h.release)

	require.Equal(t, []string{
		"session_added:s0:",
		"session_updated:s1:v3",
		"session_updated:s2:v1",
	}, h.waitDelivered(t, 3))
}

// Coalescing never moves an update across another event for the same
// session.
func TestDispatcher_CoalescingStopsAtOtherSessionEvents(t *testing.T) {
	h := newGatedHandler()
	d, _ := newTestDispatcher(t, h, 64)
	blockFirst(t, d, h)

	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s1", summary("s1", "v1")))
	require.True(t, d.Enqueue(protocol.EventSessionRemoved, "s1", protocol.SessionRemoved{SessionID: "s1"}))
	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s1", summary("s1", "v2")))
	close(h.release)

	require.Equal(t, []string{
		"session_added:s0:",
		"session_updated:s1:v1",
		"session_removed:s1",
		"session_updated:s1:v2",
	}, h.waitDelivered(t, 4))
}

// A full queue drops new events (never blocks), counts them, and warns at
// most once per interval. A coalescing update still lands.
func TestDispatcher_DropsWhenFullWithRateLimitedWarning(t *testing.T) {
	h := newGatedHandler()
	d, logs := newTestDispatcher(t, h, 3)
	blockFirst(t, d, h) // in flight; the queue itself is empty

	for i := 1; i <= 3; i++ {
		require.True(t, d.Enqueue(protocol.EventSessionUpdated, fmt.Sprint("s", i), summary(fmt.Sprint("s", i), "v1")))
	}
	for i := 4; i <= 8; i++ {
		require.False(t, d.Enqueue(protocol.EventSessionAdded, fmt.Sprint("s", i), summary(fmt.Sprint("s", i), "")))
	}
	require.True(t, d.Enqueue(protocol.EventSessionUpdated, "s2", summary("s2", "v2")), "coalesced update needs no slot")
	require.Equal(t, uint64(5), d.Dropped())
	require.Equal(t, 1, strings.Count(logs.String(), "lua: event queue full"), logs.String())

	close(h.release)
	require.Equal(t, []string{
		"session_added:s0:",
		"session_updated:s1:v1",
		"session_updated:s2:v2",
		"session_updated:s3:v1",
	}, h.waitDelivered(t, 4))
}

// Close cancels the in-flight handler call, waits for the dispatcher
// goroutine, and makes later Enqueue calls no-ops.
func TestDispatcher_CloseInterruptsAndWaits(t *testing.T) {
	h := newGatedHandler()
	d, _ := newTestDispatcher(t, h, 8)
	blockFirst(t, d, h)
	require.True(t, d.Enqueue(protocol.EventSessionRemoved, "s1", protocol.SessionRemoved{SessionID: "s1"}))

	within(t, 2*time.Second, "Close", d.Close)
	require.False(t, d.Enqueue(protocol.EventSessionRemoved, "s2", protocol.SessionRemoved{SessionID: "s2"}))
	d.Close() // idempotent
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.got, "queued events are discarded on Close")
}

// End to end with a real Runtime: a `while true do end` handler neither
// blocks Enqueue nor starves later events.
func TestDispatcher_RuntimeInfiniteLoopHandler(t *testing.T) {
	sent := make(chan sendCall, 4)
	r, err := New(Options{
		Sender:         chanSender(sent),
		Lister:         defaultLister,
		HandlerTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(r.Close)
	require.NoError(t, r.LoadFile(writeTempLua(t, `
rex.on("session_added", function(s) while true do end end)
rex.on("session_removed", function(s) rex.send(s.session_id, "removed") end)
`)))
	d, _ := newTestDispatcher(t, r, 16)

	within(t, 50*time.Millisecond, "Enqueue", func() {
		d.Enqueue(protocol.EventSessionAdded, "a", summary("a", ""))
		d.Enqueue(protocol.EventSessionRemoved, "a", protocol.SessionRemoved{SessionID: "a"})
	})
	select {
	case got := <-sent:
		require.Equal(t, sendCall{"a", "removed"}, got)
	case <-time.After(2 * time.Second):
		t.Fatal("the event after the runaway handler was never delivered")
	}
}
