package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Subscribe must reject ids the store doesn't know: they would leak an
// output subscription per request and, used as a path, read any
// transcript.log reachable from the state dir.
func TestSubscribe_RejectsUnknownSession(t *testing.T) {
	e := startRetireServer(t)
	// A transcript outside the sessions dir, reachable as ../secret.
	require.NoError(t, os.MkdirAll(filepath.Join(e.dir, "secret"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(e.dir, "secret", "transcript.log"), []byte("SECRET"), 0o644))

	conn, w, r := e.dial(t)
	for _, id := range []string{"nope", "../secret"} {
		require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "sub", protocol.Subscribe{SessionID: id, Replay: true}))
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		env, err := r.Read()
		require.NoError(t, err)
		require.Equal(t, protocol.EventError, env.Type, "id %q", id)
		ee := decodeError(t, env)
		require.Equal(t, protocol.ErrCodeUnknownSession, ee.Code)
		require.Equal(t, "sub", ee.ID)
	}
	e.srv.outputSubsMu.RLock()
	defer e.srv.outputSubsMu.RUnlock()
	require.Empty(t, e.srv.outputSubs)
}

// Output produced after the sink is registered but before the mode preamble
// goes out must follow the preamble, not vanish.
func TestSubscribe_OutputDuringPreambleFollowsIt(t *testing.T) {
	e := startRetireServer(t, doneSession("live"))
	e.srv.RegisterModePreamble("live", func() []byte {
		// The PTY reader emits while the preamble is being built.
		e.srv.broadcastSessionOutput("live", []byte("during"))
		return []byte("PRE")
	})

	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: "live", ModeSync: true}))
	var got []string
	for len(got) < 2 {
		env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionOutput })
		var so protocol.SessionOutput
		require.NoError(t, json.Unmarshal(env.Data, &so))
		got = append(got, string(so.Bytes))
	}
	require.Equal(t, []string{"PRE", "during"}, got)
}

// Subscribing twice to one session replaces the stream; output isn't doubled.
func TestSubscribe_RepeatDoesNotDuplicateOutput(t *testing.T) {
	e := startRetireServer(t, doneSession("live"))
	conn, w, r := e.dial(t)
	for range 2 {
		require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "", protocol.Subscribe{SessionID: "live"}))
	}
	// Intents run in order: once this one is answered both Subscribes are in.
	require.NoError(t, w.WriteIntent(protocol.IntentSubscribe, "sync", protocol.Subscribe{SessionID: "nope"}))
	waitEvent(t, conn, r, errorFor("sync"))

	e.srv.broadcastSessionOutput("live", []byte("one"))
	e.srv.broadcastSessionOutput("live", []byte("two"))
	var got []string
	for len(got) < 2 {
		env := waitEvent(t, conn, r, func(env protocol.Envelope) bool { return env.Type == protocol.EventSessionOutput })
		var so protocol.SessionOutput
		require.NoError(t, json.Unmarshal(env.Data, &so))
		got = append(got, string(so.Bytes))
	}
	require.Equal(t, []string{"one", "two"}, got)
}
