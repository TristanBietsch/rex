package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestNewSession_PromptOverCapRefused: an initial_prompt over 96 KiB is
// bad_intent before anything is spawned or written, instead of a failed exec
// (Linux caps one argv element at 128 KiB).
func TestNewSession_PromptOverCapRefused(t *testing.T) {
	store := state.NewStore()
	srv, sock, dir := newTestServer(t, store)
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)

	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "huge", CWD: dir,
		InitialPrompt: strings.Repeat("x", 97<<10),
	}))
	ev := readError(t, waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventError }))
	require.Equal(t, protocol.ErrCodeBadIntent, ev.Code)
	require.Contains(t, ev.Message, "initial_prompt is 99328 bytes; the limit is 98304")
	require.Empty(t, store.All(), "no session is created")
	entries, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err == nil {
		require.Empty(t, entries, "no session folder is made")
	} else {
		require.True(t, os.IsNotExist(err), err)
	}
}

func TestValidateNewSession_PromptCap(t *testing.T) {
	p := protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "s", CWD: t.TempDir()}
	p.InitialPrompt = strings.Repeat("x", protocol.MaxInitialPromptBytes)
	require.NoError(t, validateNewSession(p))
	p.InitialPrompt += "x"
	require.EqualError(t, validateNewSession(p), "initial_prompt is 98305 bytes; the limit is 98304")
}

// TestNewSession_ContextSummaryIgnoredByOldShape: the field is optional, so
// a NewSession from an older client decodes, and one that carries it
// round-trips.
func TestNewSession_ContextSummaryIgnoredByOldShape(t *testing.T) {
	var old protocol.NewSession
	require.NoError(t, json.Unmarshal([]byte(`{"tool_id":"echo","model_id":"short","slug":"s","cwd":"/tmp/x"}`), &old))
	require.Nil(t, old.ContextSummary)

	in := protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "s", CWD: "/tmp/x",
		ContextSummary: []protocol.ContextRef{{Kind: "file", Name: "a.go", Bytes: 3}, {Kind: "url", Name: "https://example.com"}}}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	require.Contains(t, string(b), `"context_summary":[{"kind":"file","name":"a.go","bytes":3},{"kind":"url","name":"https://example.com"}]`)
	var out protocol.NewSession
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in.ContextSummary, out.ContextSummary)

	b, err = json.Marshal(protocol.NewSession{ToolID: "echo"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "context_summary")
}

// TestNewSession_WithContextSummarySpawns: a daemon accepts the field and
// spawns as usual (it is metadata only and not stored).
func TestNewSession_WithContextSummarySpawns(t *testing.T) {
	srv, sock, dir := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "ctx", CWD: dir, InitialPrompt: "do it",
		ContextSummary: []protocol.ContextRef{{Kind: "paste", Name: "paste 1", Bytes: 10}},
	}))
	added := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.Equal(t, "ctx", sum.Slug)
}
