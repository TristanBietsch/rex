package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditDistance(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"ls", "ls", 0}, {"ls", "", 2}, {"", "ls", 2},
		{"lss", "ls", 1}, {"stauts", "status", 2}, {"kitten", "sitting", 3},
		{"héllo", "hello", 1},
	} {
		require.Equal(t, tc.want, editDistance(tc.a, tc.b), "%q %q", tc.a, tc.b)
	}
}

func TestSuggest(t *testing.T) {
	cmds := []string{"ls", "new", "status", "stats", "attach", "wait"}
	require.Equal(t, "ls", Suggest("lss", cmds))
	require.Equal(t, "status", Suggest("staus", cmds))
	require.Equal(t, "stats", Suggest("stat", cmds), "closest wins over the first within range")
	require.Equal(t, "attach", Suggest("atach", cmds))
	require.Empty(t, Suggest("zzzzzz", cmds), "more than two edits away")
	require.Empty(t, Suggest("x", nil))
}
