package usage

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestHub_SampleBoundsResetsAtSoEncodingNeverBreaks(t *testing.T) {
	for _, r := range []int64{math.MaxInt64, 1 << 62, 253402300800, 0, -5} {
		h, _, _, _ := quotaHub(t)
		h.Observe(sum("a", protocol.StateWorking, 1))
		h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(1), ResetsAt: r}})
		u := h.Usage()
		require.Len(t, u.Quotas, 1, r)
		require.True(t, u.Quotas[0].ResetsAt.IsZero(), "resets_at %d is dropped", r)
		_, err := json.Marshal(protocol.Snapshot{Usage: &u})
		require.NoError(t, err, r)
	}
	h, _, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{ResetsAt: 253402300799}})
	u := h.Usage()
	_, err := json.Marshal(u)
	require.NoError(t, err, "the last encodable second is kept")
	require.False(t, u.Quotas[0].ResetsAt.IsZero())
}

func TestHub_SampleClampsPercentAndRejectsBadNumbers(t *testing.T) {
	h, l, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(250)}, SevenDay: &protocol.UsageWindow{UsedPct: fp(-4)}})
	q := h.Usage().Quotas
	require.Equal(t, 100.0, *q[0].UsedPct)
	require.Equal(t, 0.0, *q[1].UsedPct)

	h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(math.NaN()), ResetsAt: 5}})
	require.Nil(t, h.Usage().Quotas[0].UsedPct, "NaN is rejected")

	for i, bad := range []float64{math.Inf(1), math.Inf(-1), math.NaN(), -1, 2e9} {
		h.Sample(protocol.UsageSample{SessionID: "a", ClaudeSessionID: fmt.Sprint("c", i), CostUSD: fp(bad),
			Spend: &protocol.UsageSpend{UsedUSD: fp(bad), LimitUSD: fp(bad), UsageWindow: protocol.UsageWindow{UsedPct: fp(1)}}})
	}
	h.Observe(sum("a", protocol.StateDone, 1))
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Nil(t, got["a"].CostUSD, "no bad cost was accepted")
	for _, qq := range h.Usage().Quotas {
		require.Nil(t, qq.UsedUSD)
		require.Nil(t, qq.LimitUSD)
	}
}

func TestHub_LargeCostsSumToAFiniteTotal(t *testing.T) {
	h, l, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	for i := 0; i < 200; i++ { // more conversations than the cap keeps
		h.Sample(protocol.UsageSample{SessionID: "a", ClaudeSessionID: fmt.Sprint("c", i), CostUSD: fp(1e9)})
	}
	h.Observe(sum("a", protocol.StateDone, 1))
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.NotNil(t, got["a"].CostUSD)
	require.False(t, math.IsInf(*got["a"].CostUSD, 0))
	require.LessOrEqual(t, *got["a"].CostUSD, float64(maxConvs)*1e9)
	_, err = json.Marshal(got["a"])
	require.NoError(t, err)
}

func TestHub_SampleCapsStrings(t *testing.T) {
	h, l, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.Sample(protocol.UsageSample{SessionID: "a", ModelID: strings.Repeat("é", 500),
		Spend: &protocol.UsageSpend{UsedUSD: fp(1), Period: strings.Repeat("x", 40)}})
	require.Equal(t, "month", h.Usage().Quotas[0].Window, "an over-long period falls back")
	h.Observe(sum("a", protocol.StateDone, 1))
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Equal(t, maxModelRunes, utf8.RuneCountInString(got["a"].Model))
}

func TestHub_QuotaWindowsAreCapped(t *testing.T) {
	h, _, _, _ := quotaHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	for i := 0; i < 100; i++ {
		h.Sample(protocol.UsageSample{SessionID: "a", Spend: &protocol.UsageSpend{UsedUSD: fp(1), Period: fmt.Sprint("p", i)}})
	}
	require.LessOrEqual(t, len(h.Usage().Quotas), maxQuotas)
}

// Two samples racing: whatever order they were applied in, the last event
// OnUsage saw is the latest state.
func TestHub_PublishOrderMatchesStateOrder(t *testing.T) {
	for round := 0; round < 20; round++ {
		h, _, _, pub := quotaHub(t)
		h.Observe(sum("a", protocol.StateWorking, 1))
		var wg sync.WaitGroup
		for i := 1; i <= 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				h.Sample(protocol.UsageSample{SessionID: "a", FiveHour: &protocol.UsageWindow{UsedPct: fp(float64(i))}})
			}(i)
		}
		wg.Wait()
		require.Equal(t, *h.Usage().Quotas[0].UsedPct, *pub.last().Quotas[0].UsedPct, "round %d", round)
	}
}
