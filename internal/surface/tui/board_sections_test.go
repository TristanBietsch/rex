package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoardGroupIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	completed := 0
	for _, g := range boardGroups {
		require.NotEmpty(t, g.ID)
		require.False(t, seen[g.ID], "duplicate id %q", g.ID)
		seen[g.ID] = true
		require.NotEmpty(t, g.Empty, "%s has no empty text", g.ID)
		if g.Completed {
			completed++
		}
	}
	require.Equal(t, 1, completed, "exactly one section holds finished work")
	require.True(t, boardGroups[groupDone].Completed)
}

func TestResolveSection(t *testing.T) {
	for _, c := range []struct {
		ref  string
		want int
		err  bool
	}{
		{"needs", groupNeeds, false},
		{"need", groupNeeds, false},
		{"3", groupDone, false},
		{"needs you", groupNeeds, false},
		{"NEEDS", groupNeeds, false},
		{"w", groupWorking, false},
		{"x", 0, true},
		{"0", 0, true},
		{"4", 0, true},
		{"", 0, true},
	} {
		got, err := resolveSection(c.ref)
		if c.err {
			require.Error(t, err, c.ref)
			require.Contains(t, err.Error(), "needs", "error lists valid ids")
			continue
		}
		require.NoError(t, err, c.ref)
		require.Equal(t, c.want, got, c.ref)
	}
}
