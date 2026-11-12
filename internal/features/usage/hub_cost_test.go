package usage

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type costCall struct {
	id  string
	usd float64
}

func costSample(id string, usd float64) protocol.UsageSample {
	return protocol.UsageSample{SessionID: id, ClaudeSessionID: "c", CostUSD: &usd}
}

func costHub(t *testing.T, on func(string, float64)) (*Hub, *clock) {
	t.Helper()
	clk := &clock{t: t0}
	h := NewHub(t.Context(), HubConfig{Ledger: newLedger(t), Now: clk.now, Tick: time.Hour, OnSessionCost: on})
	t.Cleanup(h.Close)
	return h, clk
}

func TestHub_OnSessionCostThrottle(t *testing.T) {
	var mu sync.Mutex
	var calls []costCall
	h, clk := costHub(t, func(id string, usd float64) {
		mu.Lock()
		calls = append(calls, costCall{id, usd})
		mu.Unlock()
	})
	got := func() []costCall { mu.Lock(); defer mu.Unlock(); return append([]costCall(nil), calls...) }

	// Disabled: nothing.
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(costSample("a", 0.50))
	require.Empty(t, got())

	h.SetEnabled(true)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(costSample("a", 0.10))
	clk.advance(300 * time.Millisecond)
	h.Sample(costSample("a", 0.105))
	clk.advance(300 * time.Millisecond)
	h.Sample(costSample("a", 0.12))
	require.Equal(t, []costCall{{"a", 0.10}}, got())

	clk.advance(2 * time.Second)
	h.Sample(costSample("a", 0.12))
	require.Equal(t, []costCall{{"a", 0.10}, {"a", 0.12}}, got())

	h.Sample(costSample("unknown", 3))
	require.Len(t, got(), 2)
}

func TestHub_OnSessionCostOutsideLock(t *testing.T) {
	var h *Hub
	h, _ = costHub(t, func(id string, usd float64) { h.Observe(sum(id, protocol.StateWorking, 2)) })
	h.SetEnabled(true)
	h.Observe(sum("a", protocol.StateWorking, 1))
	done := make(chan struct{})
	go func() { h.Sample(costSample("a", 1)); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Sample deadlocked: OnSessionCost ran under the hub lock")
	}
}
