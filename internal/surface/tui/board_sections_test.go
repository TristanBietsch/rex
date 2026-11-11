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

// The index constants name the screen order: Needs you, Waiting, Working,
// Done.
func TestBoardGroupOrder(t *testing.T) {
	require.Equal(t, "needs", boardGroups[groupNeeds].ID)
	require.Equal(t, "waiting", boardGroups[groupWaiting].ID)
	require.Equal(t, "working", boardGroups[groupWorking].ID)
	require.Equal(t, "done", boardGroups[groupDone].ID)
	require.Len(t, boardGroups, 4)
	w := boardGroups[groupWaiting]
	require.Equal(t, "Waiting", w.Title)
	require.Equal(t, "waiting", w.Count)
	require.Equal(t, "nothing waiting on a tool", w.Empty)
	require.False(t, w.Alert, "waiting does not want the user")
}

func TestResolveSection(t *testing.T) {
	m := boardModel()
	for _, c := range []struct {
		ref  string
		want int
		err  bool
	}{
		{"needs", groupNeeds, false},
		{"need", groupNeeds, false},
		{"1", groupNeeds, false},
		{"2", groupWaiting, false},
		{"3", groupWorking, false},
		{"4", groupDone, false},
		{"needs you", groupNeeds, false},
		{"NEEDS", groupNeeds, false},
		{"waiting", groupWaiting, false},
		{"Waiting", groupWaiting, false},
		{"wa", groupWaiting, false},
		{"working", groupWorking, false},
		{"wo", groupWorking, false},
		{"d", groupDone, false},
		{"w", 0, true}, // waiting or working
		{"x", 0, true},
		{"0", 0, true},
		{"5", 0, true},
		{"", 0, true},
	} {
		got, err := resolveSection(m, c.ref)
		if c.err {
			require.Error(t, err, c.ref)
			require.Contains(t, err.Error(), "needs", "error lists valid ids")
			continue
		}
		require.NoError(t, err, c.ref)
		require.Equal(t, c.want, got, c.ref)
	}
}
