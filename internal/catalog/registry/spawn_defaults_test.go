package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func spawnReg(t *testing.T) *Registry {
	t.Helper()
	r, err := Load("")
	require.NoError(t, err)
	return r
}

func TestResolveSpawn(t *testing.T) {
	r := spawnReg(t)
	cur := SpawnChoice{Tool: "claude", Model: "opus", Effort: "max"}
	cases := []struct {
		name                string
		tool, model, effort string
		want                SpawnChoice
		changed             bool
		errHas              string
	}{
		{name: "nothing asked", want: cur},
		{name: "effort only", effort: "low", want: SpawnChoice{"claude", "opus", "low"}, changed: true},
		{name: "model resets stale effort", model: "haiku", want: SpawnChoice{"claude", "haiku", "medium"}, changed: true},
		{name: "model keeps valid effort", model: "sonnet", want: SpawnChoice{"claude", "sonnet", "max"}, changed: true},
		{name: "new tool takes first model", tool: "antigravity", want: SpawnChoice{"antigravity", "gemini-3.8-flash", "high"}, changed: true},
		{name: "tool model effort", tool: "antigravity", model: "claude-opus-4-6", effort: "xhigh", want: SpawnChoice{"antigravity", "claude-opus-4-6", "xhigh"}, changed: true},
		{name: "model without effort clears it", tool: "antigravity", model: "gpt-oss-120b", want: SpawnChoice{"antigravity", "gpt-oss-120b", ""}, changed: true},
		{name: "unknown tool", tool: "nope", errHas: "unknown tool \"nope\" (valid: claude"},
		{name: "unknown model", model: "gpt-9", errHas: "unknown model \"gpt-9\" for tool claude (valid: opus, sonnet, haiku)"},
		{name: "unknown effort", effort: "turbo", errHas: "unknown effort \"turbo\" for claude/opus (valid: low, medium, high, xhigh, max)"},
		{name: "effort on a model without one", tool: "antigravity", model: "gpt-oss-120b", effort: "low", errHas: "takes no effort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed, err := r.ResolveSpawn(cur, tc.tool, tc.model, tc.effort)
			if tc.errHas != "" {
				require.ErrorContains(t, err, tc.errHas)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.changed, changed)
		})
	}
}

func TestResolveSpawn_UnknownToolListHidesEcho(t *testing.T) {
	_, _, err := spawnReg(t).ResolveSpawn(SpawnChoice{}, "nope", "", "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "echo")
}
