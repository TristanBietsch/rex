package client

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func outputEnv(t *testing.T, sessionID string, repaint bool) protocol.Envelope {
	t.Helper()
	data, err := json.Marshal(protocol.SessionOutput{SessionID: sessionID, Bytes: []byte("x"), Repaint: repaint})
	require.NoError(t, err)
	return protocol.Envelope{Kind: protocol.KindEvent, Type: protocol.EventSessionOutput, Data: data}
}

func replyEnv(typ, id string) protocol.Envelope {
	return protocol.Envelope{Kind: protocol.KindEvent, Type: typ, ID: id, Data: json.RawMessage(`{}`)}
}

// watch returns a RepaintWatch on session "s" and request "r1" that counts
// its nudges.
func watch(wait time.Duration) (*RepaintWatch, *atomic.Int32) {
	var n atomic.Int32
	return WatchRepaint("s", "r1", wait, func() { n.Add(1) }), &n
}

func TestRepaintWatch(t *testing.T) {
	t.Run("repaint then ack: no nudge", func(t *testing.T) {
		w, n := watch(0)
		w.Observe(outputEnv(t, "s", true))
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		require.Zero(t, n.Load())
		require.False(t, w.Nudged())
	})
	t.Run("ack without repaint: nudge once", func(t *testing.T) {
		w, n := watch(0)
		w.Observe(outputEnv(t, "s", false)) // a mode-sync preamble
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		w.Observe(outputEnv(t, "s", true))
		require.Equal(t, int32(1), n.Load())
		require.True(t, w.Nudged())
	})
	t.Run("error reply: no nudge", func(t *testing.T) {
		w, n := watch(0)
		w.Observe(replyEnv(protocol.EventError, "r1"))
		require.Zero(t, n.Load())
	})
	t.Run("other sessions and replies are ignored", func(t *testing.T) {
		w, n := watch(0)
		w.Observe(outputEnv(t, "other", true))
		w.Observe(replyEnv(protocol.EventAck, "r2"))
		w.Observe(replyEnv(protocol.EventAck, ""))
		w.Observe(protocol.Envelope{Type: protocol.EventSessionOutput, Data: json.RawMessage(`not json`)})
		require.Zero(t, n.Load())
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		require.Equal(t, int32(1), n.Load())
	})
	t.Run("no reply: nudge after the wait", func(t *testing.T) {
		w, n := watch(20 * time.Millisecond)
		require.Eventually(t, func() bool { return n.Load() == 1 }, 2*time.Second, 5*time.Millisecond)
		require.True(t, w.Nudged())
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		require.Equal(t, int32(1), n.Load())
	})
	t.Run("repaint before the wait ends: no nudge", func(t *testing.T) {
		w, n := watch(30 * time.Millisecond)
		w.Observe(outputEnv(t, "s", true))
		time.Sleep(60 * time.Millisecond)
		require.Zero(t, n.Load())
	})
	t.Run("stop: no nudge", func(t *testing.T) {
		w, n := watch(10 * time.Millisecond)
		w.Stop()
		time.Sleep(40 * time.Millisecond)
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		require.Zero(t, n.Load())
	})
	t.Run("nil watch", func(t *testing.T) {
		var w *RepaintWatch
		w.Observe(replyEnv(protocol.EventAck, "r1"))
		w.Stop()
		require.False(t, w.Nudged())
	})
}

// TestSubscribeID: the Subscribe carries its options and an ID outside the
// Do namespace, and its Ack reaches the event stream behind the repaint.
func TestSubscribeID(t *testing.T) {
	c, fd := pipeClient(t)
	sub := protocol.Subscribe{SessionID: "s", ModeSync: true, Repaint: true}
	ids := make(chan string, 1)
	go func() {
		id, err := c.SubscribeID(sub)
		if err == nil {
			ids <- id
		}
	}()
	env := fd.read(t)
	require.Equal(t, protocol.IntentSubscribe, env.Type)
	require.NotEmpty(t, env.ID)
	require.False(t, strings.HasPrefix(env.ID, c.idPrefix), "must not look like a Do reply the client drops")
	var got protocol.Subscribe
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.Equal(t, sub, got)
	require.Equal(t, env.ID, <-ids)

	fd.event(t, protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: "s", Bytes: []byte("x"), Repaint: true})
	fd.ack(t, env, "s")
	first, err := c.NextEvent()
	require.NoError(t, err)
	second, err := c.NextEvent()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSessionOutput, first.Type)
	require.Equal(t, protocol.EventAck, second.Type)
	require.Equal(t, env.ID, second.ID)

	// The two IDs differ per call.
	go func() {
		id, err := c.SubscribeID(sub)
		if err == nil {
			ids <- id
		}
	}()
	require.NotEqual(t, env.ID, fd.read(t).ID)
	<-ids
}
