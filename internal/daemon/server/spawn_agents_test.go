package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func TestBuildArgvQwen(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, model, ok := reg.FindModel("qwen", "default")
	require.True(t, ok)
	trustJSON := `{"security":{"folderTrust":{"enabled":false}}}`

	// Outside auto_trust_dirs: no trust args, no skip-permissions.
	got := buildArgv(spawnSpec{Tool: tool, Model: model, Effort: "high", SkipPermissions: true})
	require.Equal(t, []string{"qwen", "--reasoning-effort=high"}, got)

	// Trusted folder: session-only trust switch, then skip-permissions.
	args := trustDecision{Trusted: true, Args: tool.TrustArgs}
	got = buildArgv(spawnSpec{Tool: tool, Model: model, Trust: args, SkipPermissions: true})
	require.Equal(t, []string{"qwen", "--settings", trustJSON, "--dangerously-skip-permissions"}, got)
}

// Detected models become argv through model_arg_template.
func TestBuildArgvDetectedModel(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, _ := reg.Find("grok")
	m := registry.Model{ID: "grok-4.7", Name: "grok-4.7", Args: tool.ModelArgsFor("grok-4.7"), Effort: tool.ModelEffort}
	require.Equal(t, []string{"grok", "--model", "grok-4.7", "--reasoning-effort=low"},
		buildArgv(spawnSpec{Tool: tool, Model: m, Effort: "low"}))
}

// D5: rex never answers grok's or cursor's trust dialog and never passes
// --trust (it is saved globally), whatever the folder settings say.
func TestGrokCursorNoTrustHandling(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	for _, id := range []string{"grok", "cursor"} {
		tool, ok := reg.Find(id)
		require.True(t, ok)
		require.Empty(t, tool.TrustArgs, id)
		require.Nil(t, tool.TrustPrompt, id)
		model := tool.Models[0]
		argv := buildArgv(spawnSpec{Tool: tool, Model: model, Trust: trustDecision{Trusted: true}, SkipPermissions: true})
		require.NotContains(t, argv, "--trust", id)
		require.NotContains(t, argv, "-y", id)
	}
	grok, _ := reg.Find("grok")
	require.Equal(t, []string{"--always-approve"}, grok.SkipPermissionsArgs)
	cursor, _ := reg.Find("cursor")
	require.Equal(t, []string{"--yolo"}, cursor.SkipPermissionsArgs)
	require.Equal(t, []string{"cursor-agent"}, cursor.Command)
}
