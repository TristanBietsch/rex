package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
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

// @needs / @working / @done pick the one session in that state; several are
// ambiguous and none is not found. A real slug of the same name wins.
func TestResolveInSnapshot_StateAliases(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "a", State: protocol.StateNeedsInput},
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "b", State: protocol.StateWorking},
		{ID: "c0de0000-1234-4abc-89ab-cdefabcdef03", ShortID: "c0de", Slug: "c", State: protocol.StateWorking},
	}

	got, err := ResolveInSnapshot(sessions, "@needs")
	require.NoError(t, err)
	require.Equal(t, "7d4f", got.ShortID)

	var ee ExitError
	_, err = ResolveInSnapshot(sessions, "@working")
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitAmbiguousSelector, ee.Code)
	require.Contains(t, ee.Msg, "b203")
	require.Contains(t, ee.Msg, "c0de")

	_, err = ResolveInSnapshot(sessions, "@done")
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitSelectorNotFound, ee.Code)

	sessions = append(sessions, protocol.SessionSummary{ID: "d00d0000-1234-4abc-89ab-cdefabcdef04", ShortID: "d00d", Slug: "@working"})
	got, err = ResolveInSnapshot(sessions, "@working")
	require.NoError(t, err)
	require.Equal(t, "d00d", got.ShortID)
}

// Duplicate slugs (restored from an older daemon) are reported as ambiguous
// instead of silently resolving to whichever session comes first.
func TestResolveInSnapshot_DuplicateSlugIsAmbiguous(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "dup"},
		{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "dup"},
		{ID: "c0de0000-1234-4abc-89ab-cdefabcdef03", ShortID: "c0de", Slug: "solo"},
	}

	_, err := ResolveInSnapshot(sessions, "dup")
	var ee ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitAmbiguousSelector, ee.Code)
	require.Contains(t, ee.Msg, "7d4f")
	require.Contains(t, ee.Msg, "b203")

	// Short ids still pick each one.
	got, err := ResolveInSnapshot(sessions, "b203")
	require.NoError(t, err)
	require.Equal(t, "b203a999-1234-4abc-89ab-cdefabcdef02", got.ID)

	got, err = ResolveInSnapshot(sessions, "solo")
	require.NoError(t, err)
	require.Equal(t, "c0de", got.ShortID)
}
