package usage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestEstimateSource_OneEstimatedReadingPerSession(t *testing.T) {
	src := NewEstimateSource(func() []protocol.SessionSummary {
		return []protocol.SessionSummary{
			{ID: "a", ToolID: "claude", ModelID: "opus", Tokens: 1200},
			{ID: "b", ToolID: "grok", ModelID: "build", Tokens: 0},
			{ID: "c", ToolID: "grok", ModelID: "build", Tokens: 40},
		}
	})
	got, err := src.Poll(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2, "a session with no tokens has nothing to estimate")
	require.Equal(t, "a", got[0].SessionID)
	require.Equal(t, "claude", got[0].Tool)
	require.Equal(t, "opus", got[0].Model)
	require.EqualValues(t, 1200, got[0].Tokens)
	require.True(t, got[0].Estimated)
	require.Equal(t, WindowSession, got[0].Window)
	require.False(t, got[0].HasPct)
}

func TestGrokSource_RegisteredButNotImplemented(t *testing.T) {
	src := DefaultSources(func() []protocol.SessionSummary { return nil })
	require.Equal(t, []string{"estimate", "grok"}, src.Names())
	_, err := src["grok"].Poll(context.Background())
	require.ErrorIs(t, err, ErrNotImplemented)
}
