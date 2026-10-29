package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Intents that need a live PTY say which way they failed: no such session,
// a session that has exited, or a full input buffer.
func TestErrorCodes_LiveSessionIntents(t *testing.T) {
	e := startRetireServer(t, doneSession("done"))
	e.srv.RegisterInputChannel("stuck", make(chan []byte)) // nobody reads it
	conn, w, r := e.dial(t)

	cases := []struct {
		reqID, typ string
		data       any
		code       string
	}{
		{"c-unknown", protocol.IntentComplete, protocol.Complete{SessionID: "nope"}, protocol.ErrCodeUnknownSession},
		{"c-exited", protocol.IntentComplete, protocol.Complete{SessionID: "done"}, protocol.ErrCodeBadState},
		{"i-unknown", protocol.IntentSendInput, protocol.SendInput{SessionID: "nope", Bytes: []byte("x")}, protocol.ErrCodeUnknownSession},
		{"i-exited", protocol.IntentSendInput, protocol.SendInput{SessionID: "done", Bytes: []byte("x")}, protocol.ErrCodeBadState},
		{"i-full", protocol.IntentSendInput, protocol.SendInput{SessionID: "stuck", Bytes: []byte("x")}, protocol.ErrCodeBusy},
		{"r-exited", protocol.IntentReply, protocol.Reply{SessionID: "done", Text: "x"}, protocol.ErrCodeBadState},
		{"r-full", protocol.IntentReply, protocol.Reply{SessionID: "stuck", Text: "x"}, protocol.ErrCodeBusy},
	}
	for _, tc := range cases {
		require.NoError(t, w.WriteIntent(tc.typ, tc.reqID, tc.data))
		ee := decodeError(t, waitEvent(t, conn, r, errorFor(tc.reqID)))
		require.Equal(t, tc.code, ee.Code, tc.reqID)
	}
}

// SetMaxConcurrent keeps the cap within the settings range; n <= 0 used to
// remove it entirely.
func TestIntentSetMaxConcurrent_Clamped(t *testing.T) {
	e := startRetireServer(t)
	conn, w, r := e.dial(t)
	for _, tc := range []struct{ n, want int }{{-5, 1}, {0, 1}, {8, 8}, {1000, 64}} {
		require.NoError(t, w.WriteIntent(protocol.IntentSetMaxConcurrent, "", protocol.SetMaxConcurrent{N: tc.n}))
		// Intents run in order: once this is answered the cap is set.
		require.NoError(t, w.WriteIntent(protocol.IntentComplete, "sync", protocol.Complete{SessionID: "nope"}))
		waitEvent(t, conn, r, errorFor("sync"))
		require.Equal(t, tc.want, e.srv.MaxConcurrentSessions(), "n=%d", tc.n)
	}
}
