package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageSample_AbsentFieldsAreOmitted(t *testing.T) {
	b, err := json.Marshal(UsageSample{SessionID: "s"})
	require.NoError(t, err)
	require.JSONEq(t, `{"session_id":"s"}`, string(b))
	require.True(t, UsageSample{SessionID: "s", ClaudeSessionID: "c"}.Empty(), "ids alone say nothing")

	pct := 12.5
	cost := 1.0
	s := UsageSample{SessionID: "s", FiveHour: &UsageWindow{UsedPct: &pct, ResetsAt: 1900000000},
		Spend: &UsageSpend{UsageWindow: UsageWindow{ResetsAt: 5}, UsedUSD: &cost, Period: "month"}, CostUSD: &cost}
	b, err = json.Marshal(s)
	require.NoError(t, err)
	var back UsageSample
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, s, back)
	require.Contains(t, string(b), `"spend_limit":{"resets_at":5,"used_usd":1,"period":"month"}`, "the spend window is flattened")
	require.False(t, s.Empty())
}

func TestValidUsageRange(t *testing.T) {
	for _, r := range []string{"today", "7d", "30d"} {
		require.True(t, ValidUsageRange(r), r)
	}
	for _, r := range []string{"", "1d", "year", "TODAY"} {
		require.False(t, ValidUsageRange(r), r)
	}
}

func TestUsageReport_RoundTrip(t *testing.T) {
	cost := 2.5
	rep := UsageReport{
		Range: "7d", Since: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Total: UsageRow{Sessions: 2, Tokens: 90, TokensEstimated: true, CostUSD: &cost},
		Tools: []UsageToolRow{{Tool: "claude", UsageRow: UsageRow{Sessions: 2, Tokens: 90},
			Models: []UsageModelRow{{Model: "opus", UsageRow: UsageRow{Sessions: 2, Tokens: 90}}}}},
	}
	b, err := json.Marshal(rep)
	require.NoError(t, err)
	var back UsageReport
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, rep, back)
	require.Contains(t, string(b), `"tool":"claude","sessions":2`, "the row fields are inlined")
}
