package inspect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestWriteStatsJSON_HidesZeroUsageUnlessAll(t *testing.T) {
	now := time.Now()
	agg := aggregateSessions([]protocol.SessionSummary{
		{ID: "1", ToolID: "claude", ModelID: "sonnet", State: protocol.StateDone, StartedAt: now.Add(-time.Hour), LastEventAt: now},
	})
	for _, all := range []bool{false, true} {
		f, err := os.Create(filepath.Join(t.TempDir(), "out.json"))
		require.NoError(t, err)
		require.NoError(t, writeStatsJSON(f, agg, all))
		require.NoError(t, f.Close())
		raw, err := os.ReadFile(f.Name())
		require.NoError(t, err)

		var out struct {
			ByModel []struct {
				Model    string `json:"model"`
				Sessions int    `json:"sessions"`
			} `json:"by_model"`
		}
		require.NoError(t, json.Unmarshal(raw, &out), string(raw))
		var sawSonnet bool
		for _, r := range out.ByModel {
			if !all {
				require.Positive(t, r.Sessions, "zero-usage rows are hidden: %s", r.Model)
			}
			sawSonnet = sawSonnet || (r.Model == "sonnet" && r.Sessions == 1)
		}
		require.True(t, sawSonnet, string(raw))
	}
}

func TestMax(t *testing.T) {
	require.Equal(t, 3, max(3, 1))
	require.Equal(t, 3, max(1, 3))
}
