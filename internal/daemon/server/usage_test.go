package server

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func pct(v float64) *float64 { return &v }

// A client that joins late gets the current quotas in its Snapshot.
func TestHello_SnapshotCarriesUsage(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)

	require.Nil(t, srv.Usage())
	_, _, snap := helloAs(t, dialTest(t, sock), "rex-cli")
	require.Nil(t, snap.Usage, "nothing published yet")

	srv.SetUsage(protocol.Usage{Enabled: true, Quotas: []protocol.UsageQuota{
		{Tool: "claude", Window: "5h", UsedPct: pct(42), ResetsAt: time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC), At: time.Now()},
	}})
	_, _, snap = helloAs(t, dialTest(t, sock), "rex-tui")
	require.NotNil(t, snap.Usage)
	require.True(t, snap.Usage.Enabled)
	require.Len(t, snap.Usage.Quotas, 1)
	require.Equal(t, "claude", snap.Usage.Quotas[0].Tool)
	require.Equal(t, 42.0, *snap.Usage.Quotas[0].UsedPct)
}

func TestSetUsage_BroadcastsToHelloedClientsOnly(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	early := dialTest(t, sock)
	srv.SetUsage(protocol.Usage{Enabled: true})
	require.NoError(t, early.SetReadDeadline(time.Now().Add(150*time.Millisecond)))
	_, err := protocol.NewReader(early).Read()
	require.Error(t, err, "nothing before Hello")

	conn := dialTest(t, sock)
	_, r, _ := helloAs(t, conn, "rex-tui")
	srv.SetUsage(protocol.Usage{Enabled: true, Quotas: []protocol.UsageQuota{{Tool: "claude", Window: "week", UsedPct: pct(7), At: time.Now()}}})
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventUsage })
	var got protocol.Usage
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.Len(t, got.Quotas, 1)

	// The stored value is a copy.
	st := srv.Usage()
	st.Quotas[0].Tool = "mutated"
	require.Equal(t, "claude", srv.Usage().Quotas[0].Tool)
}

func TestSubscribeUsage_CancelStopsCallbacks(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	var n int
	cancel := srv.SubscribeUsage(func(protocol.Usage) { n++ })
	srv.SetUsage(protocol.Usage{Enabled: true})
	cancel()
	srv.SetUsage(protocol.Usage{})
	require.Equal(t, 1, n)
}

func TestUsageQuery_RoundTrip(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	var got protocol.UsageQuery
	srv.cfg.UsageQuery = func(q protocol.UsageQuery) (protocol.UsageReport, error) {
		got = q
		return protocol.UsageReport{Range: "7d", Total: protocol.UsageRow{Sessions: 2, Tokens: 90}, Tools: []protocol.UsageToolRow{}}, nil
	}
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")

	require.NoError(t, w.WriteIntent(protocol.IntentUsageQuery, "q1", protocol.UsageQuery{Range: "7d", Tool: "claude"}))
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == "q1" })
	require.Equal(t, protocol.EventUsageResult, env.Type)
	var rep protocol.UsageReport
	require.NoError(t, json.Unmarshal(env.Data, &rep))
	require.Equal(t, 2, rep.Total.Sessions)
	require.Equal(t, protocol.UsageQuery{Range: "7d", Tool: "claude"}, got)
}

func TestUsageQuery_ErrorsAreBadIntent(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	srv.cfg.UsageQuery = func(protocol.UsageQuery) (protocol.UsageReport, error) {
		return protocol.UsageReport{}, errors.New("usage is off")
	}
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")

	for id, q := range map[string]protocol.UsageQuery{"off": {Range: "today"}, "bad": {Range: "year"}} {
		require.NoError(t, w.WriteIntent(protocol.IntentUsageQuery, id, q))
		env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == id })
		require.Equal(t, protocol.EventError, env.Type)
		var ee protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &ee))
		require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
	}
}

// An old daemon has no handler: bad_intent "intent not implemented", which
// the client turns into "run rex daemon restart".
func TestUsageIntents_NotImplementedWithoutHooks(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	w, r, _ := helloAs(t, conn, "rex-cli")
	for _, typ := range []string{protocol.IntentUsageQuery, protocol.IntentUsageSample} {
		require.NoError(t, w.WriteIntent(typ, typ, map[string]string{"session_id": "x"}))
		env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.ID == typ })
		var ee protocol.ErrorEvent
		require.NoError(t, json.Unmarshal(env.Data, &ee))
		require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
		require.Equal(t, "intent not implemented", ee.Message)
	}
}

// The tap sends the sample with no Hello and no ID, then hangs up.
func TestUsageSample_FromATapWithoutHello(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "sess-1", ShortID: "se1", ToolID: "claude", Slug: "a", State: protocol.StateWorking}))
	srv, sock, _ := newTestServer(t, store)
	var mu sync.Mutex
	var got []protocol.UsageSample
	srv.cfg.OnUsageSample = func(s protocol.UsageSample) { mu.Lock(); got = append(got, s); mu.Unlock() }
	serveTest(t, srv)

	cost := 1.5
	send := func(s protocol.UsageSample) {
		conn := dialTest(t, sock)
		require.NoError(t, protocol.NewWriter(conn).WriteIntent(protocol.IntentUsageSample, "", s))
		require.NoError(t, conn.Close())
	}
	send(protocol.UsageSample{SessionID: "sess-1", CostUSD: &cost, ModelID: "m"})
	send(protocol.UsageSample{SessionID: "no-such-session", CostUSD: &cost}) // dropped
	send(protocol.UsageSample{SessionID: "sess-1"})                          // empty: dropped

	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 1 }, 3*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, 1)
	require.Equal(t, "sess-1", got[0].SessionID)
	require.Equal(t, 1.5, *got[0].CostUSD)
}
