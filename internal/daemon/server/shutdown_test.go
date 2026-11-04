package server

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestServe_ReturnsPromptlyWithIdleClient is the P0-02 regression: Serve
// used to wait for every handler while they sat in Read, so shutdown hung
// for as long as any client stayed connected.
func TestServe_ReturnsPromptlyWithIdleClient(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	cancel, served := serveTest(t, srv)
	idle := dialTest(t, sock)
	helloOnConn(t, idle)
	// A second client that never even says Hello.
	dialTest(t, sock)

	cancel()
	select {
	case err := <-served:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return while a client was connected")
	}
	requireDisconnected(t, idle, 2*time.Second)
}

// TestServe_WaitsForSupervisorsOnShutdown: Serve returns only after the
// per-session supervisor goroutines have exited, so their final state
// changes and meta writes land before the daemon exits.
func TestServe_WaitsForSupervisorsOnShutdown(t *testing.T) {
	store := state.NewStore()
	srv, sock, dir := newTestServer(t, store)
	cancel, served := serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r := helloOnConn(t, conn)
	require.NoError(t, w.WriteIntent(protocol.IntentNewSession, "n", protocol.NewSession{
		ToolID: "echo", ModelID: "long", Slug: "long", CWD: dir,
	}))
	added := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionAdded })
	var sum protocol.SessionSummary
	require.NoError(t, json.Unmarshal(added.Data, &sum))
	require.Eventually(t, func() bool {
		st, _ := store.CurrentState(sum.ID)
		return st == protocol.StateWorking
	}, 5*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return")
	}
	require.False(t, srv.hasStop(sum.ID), "Serve returned before the session's supervisor exited")
	st, _ := store.CurrentState(sum.ID)
	require.Equal(t, protocol.StateFailed, st, "the supervisor's final transition must land before Serve returns")
}

func TestWaitTimeout(t *testing.T) {
	var wg sync.WaitGroup
	require.True(t, waitTimeout(&wg, time.Millisecond), "an idle group is done at once")
	wg.Add(1)
	require.False(t, waitTimeout(&wg, 20*time.Millisecond))
	wg.Done()
	require.True(t, waitTimeout(&wg, time.Second))
}

// TestTrackConn_RefusesAfterShutdown: a connection accepted while Serve is
// shutting down is refused, so the handler aborts it instead of waiting in
// Read for a close that already happened.
func TestTrackConn_RefusesAfterShutdown(t *testing.T) {
	srv := &Server{}
	c, _ := pipeConn(t, 1, time.Second)
	require.True(t, srv.trackConn(c))
	srv.closeConns()
	require.True(t, c.aborted.Load())
	late, _ := pipeConn(t, 1, time.Second)
	require.False(t, srv.trackConn(late))
	late.abort(errServerShutdown)
	srv.untrackConn(c)
	c.finish()
	late.finish()
}
