package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestHandlerPanic_ClosesOnlyThatConnection: a panicking intent handler used
// to crash the daemon and every session with it. Now that connection gets
// an internal_error for the intent and is closed; the daemon and its other
// clients keep running.
func TestHandlerPanic_ClosesOnlyThatConnection(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(doneSession("s1")))
	srv, sock, _ := newTestServer(t, store)
	srv.handlers["Boom"] = func(*client, protocol.Envelope) (outcome, error) { panic("boom") }
	serveTest(t, srv)

	bystander := dialTest(t, sock)
	bw, br := helloOnConn(t, bystander)

	victim := dialTest(t, sock)
	require.NoError(t, protocol.NewWriter(victim).WriteIntent("Boom", "b1", struct{}{}))
	ev := requireErrorThenClose(t, victim, protocol.ErrCodeInternal)
	require.Equal(t, "b1", ev.ID)
	require.NotContains(t, ev.Message, "boom", "panic values stay in the daemon log")

	// The bystander's connection still works...
	require.NoError(t, bw.WriteIntent(protocol.IntentRename, "r", protocol.Rename{SessionID: "s1", Title: "still here"}))
	waitEvent(t, bystander, br, func(e protocol.Envelope) bool { return e.Type == protocol.EventSessionUpdated })
	// ...and the daemon accepts new clients.
	helloOnConn(t, dialTest(t, sock))
}
