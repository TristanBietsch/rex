package server

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// logRecord is one captured slog record.
type logRecord struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// captureLog collects every record logged through the default logger.
type captureLog struct {
	mu   sync.Mutex
	recs []logRecord
}

func (h *captureLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureLog) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *captureLog) WithGroup(string) slog.Handler            { return h }

func (h *captureLog) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{level: r.Level, msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.recs = append(h.recs, rec)
	h.mu.Unlock()
	return nil
}

// find returns the Info records whose message is msg.
func (h *captureLog) find(msg string) []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []logRecord
	for _, r := range h.recs {
		if r.level == slog.LevelInfo && r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

// installCapture routes the default logger to a capturing handler until the
// test ends.
func installCapture(t *testing.T) *captureLog {
	t.Helper()
	h := &captureLog{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func TestDispatch_LogsFirstIntentWithPeer(t *testing.T) {
	logs := installCapture(t)
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)

	idle := dialTest(t, sock) // connects and sends nothing, like a liveness probe
	conn := dialTest(t, sock)
	w, _, _ := helloAs(t, conn, "rex-cli\x1b[31m")
	require.NoError(t, w.WriteIntent(protocol.IntentFocusFilter, "", protocol.FocusFilter{}))
	_ = idle

	require.Eventually(t, func() bool { return len(logs.find("server: client")) >= 1 }, 2*time.Second, 5*time.Millisecond)
	recs := logs.find("server: client")
	require.Len(t, recs, 1, "one line per connection that sent an intent")
	a := recs[0].attrs
	for _, k := range []string{"conn", "pid", "exe", "in_session", "client"} {
		require.Contains(t, a, k)
	}
	require.NotContains(t, a["client"], "\x1b", "escape must be stripped")
	require.Contains(t, a["client"], "rex-cli")
}

func TestDispatch_LogsEveryNonReadOnlyIntent(t *testing.T) {
	logs := installCapture(t)
	srv, sock, _ := newTestServer(t, state.NewStore())
	// The handlers are replaced by no-ops: this tests dispatch, and some
	// real handlers (Shutdown, Restart) act on the daemon.
	for k := range srv.handlers {
		srv.handlers[k] = func(*client, protocol.Envelope) (outcome, error) { return outcome{}, nil }
	}
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w := protocol.NewWriter(conn)

	var want []string
	for typ := range defaultHandlers() {
		if readOnlyIntents[typ] {
			continue
		}
		want = append(want, typ)
		require.NoError(t, w.WriteIntent(typ, "", struct{}{}))
	}
	require.NotEmpty(t, want)
	require.Eventually(t, func() bool { return len(logs.find("server: intent")) >= len(want) }, 3*time.Second, 5*time.Millisecond)
	got := map[string]bool{}
	for _, r := range logs.find("server: intent") {
		got[r.attrs["type"].(string)] = true
		require.Contains(t, r.attrs, "conn")
	}
	for _, typ := range want {
		require.True(t, got[typ], "intent %s not logged", typ)
	}
	for typ := range readOnlyIntents {
		require.False(t, got[typ], "read-only intent %s must not be logged", typ)
	}
}

func TestInput_LogsBytesNeverContent(t *testing.T) {
	logs := installCapture(t)
	srv, sock, _ := newTestServer(t, state.NewStore())
	registerInput(srv, "s1", make(chan []byte, 64))
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, _, _ := helloAs(t, conn, "rex-cli")
	for range 2 {
		require.NoError(t, w.WriteIntent(protocol.IntentSendInput, "", protocol.SendInput{SessionID: "s1", Bytes: []byte("secret-123")}))
	}
	require.Eventually(t, func() bool { return len(logs.find("server: first input")) == 1 }, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return len(logs.find("server: input total")) == 1 }, 2*time.Second, 5*time.Millisecond)

	require.Len(t, logs.find("server: first input"), 1)
	tot := logs.find("server: input total")[0].attrs
	require.EqualValues(t, 20, tot["bytes"])
	require.Equal(t, "s1", tot["session"])

	logs.mu.Lock()
	defer logs.mu.Unlock()
	for _, r := range logs.recs {
		require.NotContains(t, r.msg, "secret-123")
		for _, v := range r.attrs {
			require.NotContains(t, strings.ToLower(strings.TrimSpace(asString(v))), "secret-123")
		}
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
