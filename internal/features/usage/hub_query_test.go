package usage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func fp(v float64) *float64 { return &v }

type published struct {
	mu  sync.Mutex
	evs []protocol.Usage
}

func (p *published) add(u protocol.Usage) { p.mu.Lock(); p.evs = append(p.evs, u); p.mu.Unlock() }
func (p *published) n() int               { p.mu.Lock(); defer p.mu.Unlock(); return len(p.evs) }
func (p *published) last() protocol.Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.evs[len(p.evs)-1]
}

func quotaHub(t *testing.T) (*Hub, *Ledger, *clock, *published) {
	t.Helper()
	clk := &clock{t: t0}
	l := newLedger(t)
	pub := &published{}
	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Hour, OnUsage: pub.add})
	t.Cleanup(h.Close)
	h.SetEnabled(true)
	return h, l, clk, pub
}

func TestHub_SampleKeepsQuotasAndPublishesOnlyChanges(t *testing.T) {
	h, _, clk, pub := quotaHub(t)
	require.Equal(t, 1, pub.n(), "switching on publishes enabled")
	h.Observe(sum("a", protocol.StateWorking, 10))

	reset := t0.Add(2 * time.Hour).Unix()
	s := protocol.UsageSample{SessionID: "a", ClaudeSessionID: "c1", CostUSD: fp(1),
		FiveHour: &protocol.UsageWindow{UsedPct: fp(42), ResetsAt: reset},
		SevenDay: &protocol.UsageWindow{UsedPct: fp(18)}}
	h.Sample(s)
	require.Equal(t, 2, pub.n())
	q := pub.last().Quotas
	require.Len(t, q, 2)
	require.Equal(t, "5h", q[0].Window, "5h sorts before week")
	require.Equal(t, 42.0, *q[0].UsedPct)
	require.True(t, q[0].ResetsAt.Equal(time.Unix(reset, 0)))
	require.Equal(t, "claude", q[0].Tool)
	require.Equal(t, "week", q[1].Window)
	require.True(t, q[1].ResetsAt.IsZero(), "no reset time reported")

	clk.advance(time.Minute)
	h.Sample(s) // the same numbers again
	require.Equal(t, 2, pub.n(), "an unchanged reading is not republished")

	s.FiveHour = &protocol.UsageWindow{UsedPct: fp(43), ResetsAt: reset}
	h.Sample(s)
	require.Equal(t, 3, pub.n())
	require.Equal(t, 43.0, *h.Usage().Quotas[0].UsedPct)
}

func TestHub_SampleSpendLimitOnly(t *testing.T) {
	h, _, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", Spend: &protocol.UsageSpend{UsedUSD: fp(5), LimitUSD: fp(50), Period: "Month"}})
	q := h.Usage().Quotas
	require.Len(t, q, 1)
	require.Equal(t, "month", q[0].Window)
	require.Equal(t, 5.0, *q[0].UsedUSD)
	require.Equal(t, 50.0, *q[0].LimitUSD)
	require.Nil(t, q[0].UsedPct)

	h.Sample(protocol.UsageSample{SessionID: "a", Spend: &protocol.UsageSpend{UsedUSD: fp(6), Period: "weird period!\x1b[31m"}})
	require.Equal(t, "month", h.Usage().Quotas[0].Window, "an odd period falls back to month")
}

func TestHub_SampleWithoutRateLimitsStillFeedsCostAndModel(t *testing.T) {
	h, l, _, pub := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	n := pub.n()
	h.Sample(protocol.UsageSample{SessionID: "a", ClaudeSessionID: "c", CostUSD: fp(3), ModelID: "m\x1b[31m-x"})
	require.Equal(t, n, pub.n(), "API-key billing: no quota, nothing to publish")
	require.Empty(t, h.Usage().Quotas)
	h.Observe(sum("a", protocol.StateDone, 1))
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.InDelta(t, 3, *got["a"].CostUSD, 1e-9)
	require.Equal(t, "m-x", got["a"].Model, "escape bytes never reach the ledger")
}

func TestHub_SampleIgnoredForUnknownSession(t *testing.T) {
	h, _, _, pub := quotaHub(t)
	n := pub.n()
	h.Sample(protocol.UsageSample{SessionID: "ghost", FiveHour: &protocol.UsageWindow{UsedPct: fp(1)}})
	require.Equal(t, n, pub.n())
	require.Empty(t, h.Usage().Quotas)
}

func TestHub_StaleQuotas(t *testing.T) {
	h, _, clk, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(1)}})
	require.False(t, h.Usage().Quotas[0].Stale)
	clk.advance(StaleAfter + time.Second)
	require.True(t, h.Usage().Quotas[0].Stale)
}

func TestHub_SwitchingOffClearsAndPublishes(t *testing.T) {
	h, _, _, pub := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(1)}})
	h.SetEnabled(false)
	require.False(t, pub.last().Enabled)
	require.Empty(t, pub.last().Quotas)
	require.Empty(t, h.Usage().Quotas)
	_, err := h.Query(protocol.UsageQuery{})
	require.ErrorIs(t, err, ErrOff)
}

func TestAggregate_PerToolPerModelTotal(t *testing.T) {
	since := t0.Add(-24 * time.Hour)
	withCost := func(r Record, c float64) Record { r.CostUSD = &c; return r }
	recs := map[string]Record{
		"1": withCost(Record{SessionID: "1", Tool: "claude", Model: "opus", Tokens: 400, Estimated: true, At: t0}, 11.9),
		"2": withCost(Record{SessionID: "2", Tool: "claude", Model: "sonnet", Tokens: 800, Estimated: true, At: t0}, 6.5),
		"3": withCost(Record{SessionID: "3", Tool: "claude", Model: "sonnet", Tokens: 100, At: t0}, 0.5),
		"4": {SessionID: "4", Tool: "grok", Model: "build", Tokens: 90, Estimated: true, At: t0},
		"5": {SessionID: "5", Tool: "claude", Model: "opus", Tokens: 99999, At: since.Add(-time.Hour)}, // out of range
	}
	rep := Aggregate(recs, "today", since, "", nil)
	require.Equal(t, 4, rep.Total.Sessions)
	require.EqualValues(t, 1390, rep.Total.Tokens)
	require.True(t, rep.Total.TokensEstimated)
	require.InDelta(t, 18.9, *rep.Total.CostUSD, 1e-9)

	require.Len(t, rep.Tools, 2)
	require.Equal(t, "claude", rep.Tools[0].Tool, "most tokens first")
	require.Equal(t, 3, rep.Tools[0].Sessions)
	require.Len(t, rep.Tools[0].Models, 2)
	require.Equal(t, "sonnet", rep.Tools[0].Models[0].Model)
	require.EqualValues(t, 900, rep.Tools[0].Models[0].Tokens)
	require.InDelta(t, 7.0, *rep.Tools[0].Models[0].CostUSD, 1e-9)
	require.Nil(t, rep.Tools[1].CostUSD, "grok reported no cost")

	only := Aggregate(recs, "today", since, "grok", nil)
	require.Equal(t, 1, only.Total.Sessions)
	require.Len(t, only.Tools, 1)
	require.Equal(t, "grok", only.Tool)

	none := Aggregate(nil, "today", since, "", nil)
	require.Empty(t, none.Tools)
	require.NotNil(t, none.Tools, "an empty report lists no tools, as [] not null")
	require.Nil(t, none.Total.CostUSD)
}

func TestRangeStart(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 30, 0, 0, time.UTC)
	today, err := RangeStart("", now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC), today)
	d7, _ := RangeStart("7d", now)
	require.Equal(t, now.AddDate(0, 0, -7), d7)
	d30, _ := RangeStart("30d", now)
	require.Equal(t, now.AddDate(0, 0, -30), d30)
	_, err = RangeStart("year", now)
	require.ErrorIs(t, err, ErrBadRange)
}

// A running session counts with its current totals, not its last line; an
// ended session whose process is long gone counts from the ledger alone.
func TestHub_QueryMergesLedgerAndLiveSessions(t *testing.T) {
	h, l, clk, _ := quotaHub(t)
	require.NoError(t, l.Append(Record{SessionID: "gone", Tool: "grok", Model: "build", Tokens: 90, Estimated: true, At: t0.Add(-2 * 24 * time.Hour), Ended: true}))
	require.NoError(t, l.Append(Record{SessionID: "a", Tool: "claude", Model: "opus", Tokens: 100, Estimated: true, At: t0.Add(-time.Hour)}))
	h.Observe(sum("a", protocol.StateWorking, 700)) // grew since its periodic line
	clk.advance(time.Minute)

	rep, err := h.Query(protocol.UsageQuery{Range: "7d"})
	require.NoError(t, err)
	require.Equal(t, 2, rep.Total.Sessions)
	require.EqualValues(t, 790, rep.Total.Tokens, "a is counted once, at its live total")

	today, err := h.Query(protocol.UsageQuery{})
	require.NoError(t, err)
	require.Equal(t, "today", today.Range)
	require.Equal(t, 1, today.Total.Sessions, "the grok session ended two days ago")

	_, err = h.Query(protocol.UsageQuery{Range: "year"})
	require.ErrorIs(t, err, ErrBadRange)
}

func TestHub_QueryAfterPruneStillCounts(t *testing.T) {
	h, _, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 321))
	h.Removed("a")
	h.Sync()
	rep, err := h.Query(protocol.UsageQuery{Range: "7d"})
	require.NoError(t, err)
	require.EqualValues(t, 321, rep.Total.Tokens)
}

func TestHub_QueryOffAndNil(t *testing.T) {
	h := NewHub(context.Background(), HubConfig{Ledger: newLedger(t)})
	_, err := h.Query(protocol.UsageQuery{})
	require.ErrorIs(t, err, ErrOff)
	var nilHub *Hub
	_, err = nilHub.Query(protocol.UsageQuery{})
	require.ErrorIs(t, err, ErrOff)
	require.Equal(t, protocol.Usage{}, nilHub.Usage())
	nilHub.Sample(protocol.UsageSample{SessionID: "a"})
	nilHub.Report("a", "", nil, "")
	nilHub.Removed("a")
	h.Close()
}
