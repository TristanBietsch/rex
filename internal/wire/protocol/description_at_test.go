package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionSummary_DescriptionAtWire(t *testing.T) {
	var zero SessionSummary
	b, err := json.Marshal(zero)
	require.NoError(t, err)
	require.NotContains(t, string(b), "description_at", "omitted when there is no description yet")

	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	b, err = json.Marshal(SessionSummary{Description: "Running tests", DescriptionAt: at})
	require.NoError(t, err)
	require.Contains(t, string(b), `"description_at":"2026-03-10T15:30:00Z"`)

	var back SessionSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.True(t, at.Equal(back.DescriptionAt))

	// An older daemon never sends the field.
	var old SessionSummary
	require.NoError(t, json.Unmarshal([]byte(`{"description":"x"}`), &old))
	require.True(t, old.DescriptionAt.IsZero())
}
