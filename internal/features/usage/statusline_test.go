package usage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The fixtures are synthetic, built from the documented schema.

func f64(v float64) *float64 { return &v }

func TestParseStatusLine_Full(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{
	  "session_id": "conv-1",
	  "cwd": "/work/project",
	  "transcript_path": "/secret/transcript.jsonl",
	  "model": {"id": "claude-opus-x", "display_name": "Opus X"},
	  "cost": {"total_cost_usd": 1.25, "total_duration_ms": 9000},
	  "rate_limits": {
	    "five_hour": {"used_percentage": 42.5, "resets_at": 1900000000},
	    "seven_day": {"used_percentage": 18, "resets_at": 1900500000},
	    "spend_limit": {"used_percentage": 10, "resets_at": 1901000000, "used_usd": 5, "limit_usd": 50, "period": "month"}
	  }
	}`))
	require.True(t, ok)
	require.Equal(t, "conv-1", s.ClaudeSessionID)
	require.Equal(t, "claude-opus-x", s.ModelID)
	require.Equal(t, "Opus X", s.ModelName)
	require.Equal(t, f64(1.25), s.CostUSD)
	require.Equal(t, f64(42.5), s.FiveHour.UsedPct)
	require.EqualValues(t, 1900000000, s.FiveHour.ResetsAt)
	require.Equal(t, f64(18), s.SevenDay.UsedPct)
	require.NotNil(t, s.Spend)
	require.Equal(t, f64(5), s.Spend.UsedUSD)
	require.Equal(t, f64(50), s.Spend.LimitUSD)
	require.Equal(t, "month", s.Spend.Period)
	require.Equal(t, f64(10), s.Spend.UsedPct)
}

func TestParseStatusLine_RateLimitsAbsent(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{"session_id":"c","model":{"id":"m"},"cost":{"total_cost_usd":0.5}}`))
	require.True(t, ok)
	require.Nil(t, s.FiveHour)
	require.Nil(t, s.SevenDay)
	require.Nil(t, s.Spend)
	require.Equal(t, f64(0.5), s.CostUSD)
	require.False(t, s.Empty())
}

func TestParseStatusLine_PartiallyPresent(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{"rate_limits":{"five_hour":{"used_percentage":7}, "seven_day":{"resets_at":1900500000}}}`))
	require.True(t, ok)
	require.Equal(t, f64(7), s.FiveHour.UsedPct)
	require.Zero(t, s.FiveHour.ResetsAt)
	require.Nil(t, s.SevenDay.UsedPct)
	require.EqualValues(t, 1900500000, s.SevenDay.ResetsAt)
}

func TestParseStatusLine_SpendLimitOnly(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{"rate_limits":{"spend_limit":{"used_usd":12.5,"limit_usd":100,"period":"month"}}}`))
	require.True(t, ok)
	require.Nil(t, s.FiveHour)
	require.Nil(t, s.SevenDay)
	require.Equal(t, f64(12.5), s.Spend.UsedUSD)
	require.Nil(t, s.Spend.UsedPct)
	require.False(t, s.Empty())
}

func TestParseStatusLine_AllNull(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{
	  "session_id": null, "model": null, "cost": {"total_cost_usd": null},
	  "rate_limits": {"five_hour": null, "seven_day": {"used_percentage": null, "resets_at": null},
	                  "spend_limit": {"used_percentage": null, "resets_at": null, "used_usd": null, "limit_usd": null, "period": null}}
	}`))
	require.True(t, ok)
	require.True(t, s.Empty(), "%+v", s)
}

func TestParseStatusLine_OddValuesAreSkipped(t *testing.T) {
	s, ok := ParseStatusLine([]byte(`{"cost":{"total_cost_usd":"1.0"},"rate_limits":{"five_hour":{"used_percentage":-3,"resets_at":"soon"}},"model":{"id":7}}`))
	require.True(t, ok)
	require.True(t, s.Empty(), "%+v", s)
}

func TestParseStatusLine_ResetsAtIsEpochSeconds(t *testing.T) {
	// A milliseconds value is not seconds: it is dropped, not misread.
	s, _ := ParseStatusLine([]byte(`{"rate_limits":{"five_hour":{"used_percentage":1,"resets_at":1900000000000}}}`))
	require.Zero(t, s.FiveHour.ResetsAt)
}

func TestParseStatusLine_NotAnObject(t *testing.T) {
	for _, in := range []string{``, `nope`, `[1]`, `null`, `"x"`} {
		_, ok := ParseStatusLine([]byte(in))
		require.False(t, ok, in)
	}
}
