package core

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fuzzSessions is a fixed board with the shapes that make selectors hard:
// IDs sharing a 4-hex prefix (extended short ids), a hex-looking slug that
// is also another session's ID prefix, and a duplicated slug left by an older
// daemon.
var fuzzSessions = []protocol.SessionSummary{
	{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha"},
	{ID: "7d4f9999-1234-4abc-89ab-cdefabcdef02", ShortID: "7d4f9", Slug: "alpha-2"},
	{ID: "b203a999-1234-4abc-89ab-cdefabcdef03", ShortID: "b203", Slug: "beta"},
	{ID: "beefcafe-1234-4abc-89ab-cdefabcdef04", ShortID: "beef", Slug: "b203a"},
	{ID: "c0de0000-1234-4abc-89ab-cdefabcdef05", ShortID: "c0de", Slug: "dup"},
	{ID: "d00d0000-1234-4abc-89ab-cdefabcdef06", ShortID: "d00d", Slug: "dup"},
}

// selectorOutcome is the comparable result of one resolution: the session ID
// on success, or the exit code on failure.
type selectorOutcome struct {
	id   string
	code int
}

// resolveOutcome runs ResolveInSnapshot and folds its result into a
// selectorOutcome, failing the test if the error is not an ExitError.
func resolveOutcome(t *testing.T, sessions []protocol.SessionSummary, sel string) selectorOutcome {
	t.Helper()
	got, err := ResolveInSnapshot(sessions, sel)
	if err == nil {
		return selectorOutcome{id: got.ID}
	}
	var ee ExitError
	require.True(t, errors.As(err, &ee), "selector %q: error %T is not an ExitError", sel, err)
	return selectorOutcome{code: ee.Code}
}

// FuzzResolveInSnapshot checks that selector resolution over a fixed board
// never panics, only resolves to a session the selector actually names, keeps
// the documented precedence (full ID, then exact slug unless it collides with another session short id), and does not depend
// on the order sessions arrive in.
func FuzzResolveInSnapshot(f *testing.F) {
	for _, s := range []string{
		"alpha", "b203", "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", "nope", "dup",
		"", "7d4f", "7d4f9", "b203a", "beef", "7D4F", "B203A", "7d4f3c8a-1234-4abc-89ab-cdefabcdef0",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, sel string) {
		out := resolveOutcome(t, fuzzSessions, sel)

		reversed := slices.Clone(fuzzSessions)
		slices.Reverse(reversed)
		require.Equal(t, out, resolveOutcome(t, reversed, sel), "outcome depends on session order")

		if out.id == "" {
			switch out.code {
			case ExitInvalidArgs:
				require.Empty(t, sel, "only the empty selector is invalid")
			case ExitSelectorNotFound, ExitAmbiguousSelector:
			default:
				t.Fatalf("selector %q: unexpected exit code %d", sel, out.code)
			}
		}

		var bySlug []protocol.SessionSummary
		for _, s := range fuzzSessions {
			if s.ID == sel {
				require.Equal(t, s.ID, out.id, "full id %q must resolve to its session", sel)
				return
			}
			if s.Slug == sel {
				bySlug = append(bySlug, s)
			}
		}
		// Another session whose short id or id prefix the selector spells
		// (in any case) makes an exact slug ambiguous instead of winning.
		lower := strings.ToLower(sel)
		collides := false
		if hexShortID.MatchString(lower) {
			for _, s := range fuzzSessions {
				if (s.ShortID == lower || strings.HasPrefix(s.ID, lower)) && s.Slug != sel {
					collides = true
				}
			}
		}
		switch {
		case len(bySlug) == 0:
		case len(bySlug) == 1 && !collides:
			// An exact slug beats a short-id match on the same session only.
			require.Equal(t, bySlug[0].ID, out.id, "exact slug %q must win", sel)
			return
		default:
			require.Equal(t, ExitAmbiguousSelector, out.code, "duplicate or colliding slug %q must be ambiguous", sel)
			return
		}

		if out.id != "" {
			i := slices.IndexFunc(fuzzSessions, func(s protocol.SessionSummary) bool { return s.ID == out.id })
			require.GreaterOrEqual(t, i, 0, "resolved to a session outside the snapshot")
			got := fuzzSessions[i]
			require.True(t, got.ShortID == lower || strings.HasPrefix(got.ID, lower),
				"selector %q resolved to %s (%s) without naming it", sel, got.ShortID, got.Slug)
		}
	})
}
