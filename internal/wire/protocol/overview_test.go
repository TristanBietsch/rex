package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOverview_RoundTrip(t *testing.T) {
	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	in := Overview{Enabled: true, Headline: "2 ready for review", GeneratedAt: at}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":true,"headline":"2 ready for review","generated_at":"2026-03-10T15:30:00Z"}`, string(b))

	var back Overview
	require.NoError(t, json.Unmarshal(b, &back))
	require.True(t, back.Enabled)
	require.Equal(t, in.Headline, back.Headline)
	require.True(t, at.Equal(back.GeneratedAt))

	b, err = json.Marshal(Overview{})
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":false}`, string(b), "headline and time omitted when empty")
}

func TestSnapshot_OverviewOmittedWhenNil(t *testing.T) {
	b, err := json.Marshal(Snapshot{Sessions: []SessionSummary{}})
	require.NoError(t, err)
	require.NotContains(t, string(b), "overview")

	b, err = json.Marshal(Snapshot{Overview: &Overview{Enabled: true, Headline: "x"}})
	require.NoError(t, err)
	require.Contains(t, string(b), `"overview":{"enabled":true,"headline":"x"}`)

	// An older daemon's snapshot has no overview.
	var old Snapshot
	require.NoError(t, json.Unmarshal([]byte(`{"sessions":[],"filter":"all"}`), &old))
	require.Nil(t, old.Overview)
}

func TestSessionSummary_WaitReasonWire(t *testing.T) {
	b, err := json.Marshal(SessionSummary{})
	require.NoError(t, err)
	require.NotContains(t, string(b), "wait_reason")

	b, err = json.Marshal(SessionSummary{WaitReason: WaitReview})
	require.NoError(t, err)
	require.Contains(t, string(b), `"wait_reason":"review"`)

	var back SessionSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, WaitReview, back.WaitReason)
	require.Equal(t, "input", WaitInput)
}
