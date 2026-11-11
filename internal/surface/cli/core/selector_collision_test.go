package core

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// "feed" is a valid slug and also valid hex: it must not silently beat the
// session whose id starts with feed.
func TestResolve_SlugVsHexPrefixAmbiguous(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "1111aaaa-1234-4abc-89ab-cdefabcdef01", ShortID: "1111", Slug: "feed"},
		{ID: "feedbeef-1234-4abc-89ab-cdefabcdef02", ShortID: "feed", Slug: "other"},
	}
	_, err := ResolveInSnapshot(sessions, "feed")
	var ee ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitAmbiguousSelector, ee.Code)
	require.Contains(t, ee.Msg, "1111 (slug)")
	require.Contains(t, ee.Msg, "feed (short id)")
	require.Contains(t, ee.Msg, "full id")
	require.NotEmpty(t, ee.Hint)

	// The full id still picks either one.
	got, err := ResolveInSnapshot(sessions, "feedbeef-1234-4abc-89ab-cdefabcdef02")
	require.NoError(t, err)
	require.Equal(t, "other", got.Slug)

	// A longer prefix no longer collides with the slug.
	got, err = ResolveInSnapshot(sessions, "feedb")
	require.NoError(t, err)
	require.Equal(t, "other", got.Slug)
}

// The same session reached by slug and by short id is not a collision.
func TestResolve_SlugAndShortIDOfOneSession(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "feedbeef-1234-4abc-89ab-cdefabcdef02", ShortID: "feed", Slug: "feed"},
		{ID: "1111aaaa-1234-4abc-89ab-cdefabcdef01", ShortID: "1111", Slug: "x"},
	}
	got, err := ResolveInSnapshot(sessions, "feed")
	require.NoError(t, err)
	require.Equal(t, "feed", got.ShortID)
}

func TestResolve_UppercaseShortID(t *testing.T) {
	sessions := []protocol.SessionSummary{
		{ID: "a91cbeef-1234-4abc-89ab-cdefabcdef01", ShortID: "a91c", Slug: "alpha"},
	}
	for _, sel := range []string{"A91C", "a91C", "A91CBE"} {
		got, err := ResolveInSnapshot(sessions, sel)
		require.NoError(t, err, sel)
		require.Equal(t, "a91c", got.ShortID)
	}
	// An uppercase slug is still matched as written.
	sessions[0].Slug = "Alpha"
	got, err := ResolveInSnapshot(sessions, "Alpha")
	require.NoError(t, err)
	require.Equal(t, "a91c", got.ShortID)
	_, err = ResolveInSnapshot(sessions, "alpha")
	require.Error(t, err)
}

func TestHelp_NoAtAliases(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w
	runErr := RunHelp()
	os.Stdout = saved
	require.NoError(t, w.Close())
	require.NoError(t, runErr)
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	for _, alias := range []string{"@needs", "@working", "@done"} {
		require.NotContains(t, string(out), alias)
	}
	require.Contains(t, string(out), "selectors: short id, slug, full id")
}
