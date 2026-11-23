package registry

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func envPassTool(pass ...string) Tool {
	return Tool{ID: "x", Models: []Model{{ID: "m"}}, EnvPass: pass,
		Detect: Detect{Kind: "heuristic", PromptRegex: "p", IdleMs: 100}}
}

func TestEnvPassValidation(t *testing.T) {
	require.NoError(t, validate([]Tool{envPassTool("OK_1", "AWS_*", "_x")}))
	for _, bad := range []string{"BAD NAME", "", "*", "1A", "A-B", "A=b", "A*B", "**", "A**"} {
		require.ErrorContains(t, validate([]Tool{envPassTool(bad)}), "env_pass", bad)
	}
}

func TestBuiltinEnvPass(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	claude, _ := reg.Find("claude")
	require.Subset(t, claude.EnvPass, []string{
		"ANTHROPIC_*", "ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_GIT_BASH_PATH", "AWS_*", "ANTHROPIC_VERTEX_PROJECT_ID",
		"CLOUD_ML_REGION", "GOOGLE_APPLICATION_CREDENTIALS",
	})
	codex, _ := reg.Find("codex")
	require.Contains(t, codex.EnvPass, "OPENAI_API_KEY")
	ollama, _ := reg.Find("ollama")
	require.Subset(t, ollama.EnvPass, []string{"OLLAMA_HOST", "OLLAMA_API_KEY"})
	secrets := []string{"GH_TOKEN", "GITHUB_TOKEN", "NPM_TOKEN", "*"}
	for _, tool := range reg.Tools {
		for _, s := range secrets {
			require.NotContains(t, tool.EnvPass, s, tool.ID)
		}
		require.NoError(t, validateEnvPass(tool), tool.ID)
	}
}

func TestMergeEnvPassUnion(t *testing.T) {
	baseList := make([]string, 2, 8) // spare capacity: an append onto it would show
	copy(baseList, []string{"A", "B"})
	base := Tool{ID: "a", EnvPass: baseList}
	m := mergeOne(base, Tool{ID: "a", EnvPass: []string{"C", "A", "C"}})
	require.Equal(t, []string{"A", "B", "C"}, m.EnvPass, "built-in names first, overlay names added once")
	require.Equal(t, []string{"A", "B"}, base.EnvPass)
	require.Equal(t, []string{"A", "B", ""}, slices.Concat(baseList, baseList[len(baseList):cap(baseList)][:1]),
		"registry slice's spare capacity untouched")
	require.Equal(t, []string{"A", "B"}, mergeOne(base, Tool{ID: "a", Name: "n"}).EnvPass)
}

func TestMergeEnvPassKeepsBuiltinNames(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	claude, _ := reg.Find("claude")
	merged := mergeOne(claude, Tool{ID: "claude", EnvPass: []string{"MY_PROXY_TOKEN"}})
	require.Subset(t, merged.EnvPass, claude.EnvPass)
	require.Contains(t, merged.EnvPass, "MY_PROXY_TOKEN")
	require.Contains(t, merged.EnvPass, "ANTHROPIC_*")
	require.Contains(t, merged.EnvPass, "AWS_*")
}

func TestAllEnvPass_Union(t *testing.T) {
	r := &Registry{Tools: []Tool{
		{ID: "a", EnvPass: []string{"B", "A_*"}},
		{ID: "b", EnvPass: []string{"A_*", "C"}},
		{ID: "c"},
	}}
	require.Equal(t, []string{"A_*", "B", "C"}, AllEnvPass(r))
	require.Empty(t, AllEnvPass(&Registry{}))
	require.Empty(t, AllEnvPass(nil))
	require.Equal(t, []string{"B", "A_*"}, r.Tools[0].EnvPass, "input unchanged")
}
