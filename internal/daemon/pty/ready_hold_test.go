package pty

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	working = protocol.StateWorking
	ready   = protocol.StateNeedsInput
)

func TestReadyHold_HoldsThenPublishes(t *testing.T) {
	h := readyHold{hold: readyHoldDefault}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ok, why := h.admit(working, ready, false, t0)
	require.False(t, ok)
	require.Equal(t, "hold", why)
	ok, _ = h.admit(working, ready, false, t0.Add(readyHoldDefault-time.Millisecond))
	require.False(t, ok)
	ok, why = h.admit(working, ready, false, t0.Add(readyHoldDefault))
	require.True(t, ok)
	require.Equal(t, "held", why)
	require.False(t, h.pending())
}

func TestReadyHold_FlapCancels(t *testing.T) {
	h := readyHold{hold: readyHoldDefault}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ok, _ := h.admit(working, ready, false, t0)
	require.False(t, ok)
	ok, why := h.admit(working, working, false, t0.Add(time.Second))
	require.False(t, ok, "no change to publish")
	require.Equal(t, "flap", why)
	// The next entry starts a fresh hold.
	ok, _ = h.admit(working, ready, false, t0.Add(1500*time.Millisecond))
	require.False(t, ok)
	ok, _ = h.admit(working, ready, false, t0.Add(3*time.Second))
	require.False(t, ok, "only 1.5s since the new entry")
	ok, _ = h.admit(working, ready, false, t0.Add(3500*time.Millisecond))
	require.True(t, ok)
}

func TestReadyHold_QuestionPublishesAtOnce(t *testing.T) {
	h := readyHold{hold: readyHoldDefault}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ok, why := h.admit(working, ready, true, t0)
	require.True(t, ok)
	require.Equal(t, "question", why)
	// A question arriving during a hold publishes too.
	_, _ = h.admit(working, ready, false, t0)
	ok, why = h.admit(working, ready, true, t0.Add(time.Second))
	require.True(t, ok)
	require.Equal(t, "question", why)
	require.False(t, h.pending())
}

func TestReadyHold_OtherTransitionsPassThrough(t *testing.T) {
	h := readyHold{hold: readyHoldDefault}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ from, to protocol.State }{
		{ready, working},
		{working, protocol.StateDone},
		{ready, protocol.StateDone},
		{protocol.StateQueued, ready},
	} {
		ok, why := h.admit(c.from, c.to, false, t0)
		require.True(t, ok, "%s→%s", c.from, c.to)
		require.Equal(t, "adapter", why)
	}
	ok, why := h.admit(ready, ready, false, t0)
	require.False(t, ok)
	require.Empty(t, why)
}

func TestReadyHold_ZeroHoldDisables(t *testing.T) {
	h := readyHold{}
	ok, why := h.admit(working, ready, false, time.Now())
	require.True(t, ok)
	require.Equal(t, "adapter", why)
}
