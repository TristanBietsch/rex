package ids

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSessionID_FormatAndUniqueness(t *testing.T) {
	uuidRE := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := NewSessionID()
		require.Regexp(t, uuidRE, id)
		_, dup := seen[id]
		require.False(t, dup, "duplicate id %s", id)
		seen[id] = struct{}{}
	}
}

func TestShortID_FirstFourHex(t *testing.T) {
	id := "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	require.Equal(t, "7d4f", ShortID(id))
}

func TestExtendShortID_GrowsOnCollision(t *testing.T) {
	a := "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	b := "7d4f9999-1234-4abc-89ab-cdefabcdef02" // shares first 4 hex with a
	taken := map[string]struct{}{ShortID(a): {}}
	// ExtendShortID(b, taken) must return at least 5 chars
	got := ExtendShortID(b, taken)
	require.GreaterOrEqual(t, len(got), 5)
	require.NotEqual(t, ShortID(a), got)
}
