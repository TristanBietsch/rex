package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestApplyPatch_CostUSD(t *testing.T) {
	var s protocol.SessionSummary
	applyPatch(&s, map[string]any{"cost_usd": 0.42})
	require.NotNil(t, s.CostUSD)
	require.InDelta(t, 0.42, *s.CostUSD, 1e-9)
	applyPatch(&s, map[string]any{"cost_usd": nil})
	require.Nil(t, s.CostUSD)
}
