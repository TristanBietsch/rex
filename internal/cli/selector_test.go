package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestResolveInSnapshot(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha"},
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "beta"},
	}

	got, err := ResolveInSnapshot(sessions, "alpha")
	require.NoError(t, err)
	require.Equal(t, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", got.ID)

	got, err = ResolveInSnapshot(sessions, "b203")
	require.NoError(t, err)
	require.Equal(t, "beta", got.Slug)

	got, err = ResolveInSnapshot(sessions, "7d4f3c8a-1234-4abc-89ab-cdefabcdef01")
	require.NoError(t, err)
	require.Equal(t, "alpha", got.Slug)

	_, err = ResolveInSnapshot(sessions, "nope")
	require.Error(t, err)
	require.IsType(t, ExitError{}, err)
}
