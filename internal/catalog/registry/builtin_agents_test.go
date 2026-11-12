package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinNewAgents(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	get := func(id string) Tool {
		tool, ok := reg.Find(id)
		require.True(t, ok, id)
		// Hidden until the TUI honors extra_agents and Installed.
		require.NotNil(t, tool.EnabledByDefault, id)
		require.False(t, *tool.EnabledByDefault, id)
		require.NotEmpty(t, tool.Models, id)
		return tool
	}

	qwen := get("qwen")
	require.Equal(t, []string{"qwen"}, qwen.Command)
	require.Equal(t, StatusOSC7501, qwen.Detect.Status)
	require.Equal(t, []string{"--prompt-interactive={prompt}"}, qwen.PromptArgs)
	require.Equal(t, []string{"--dangerously-skip-permissions"}, qwen.SkipPermissionsArgs)
	require.Equal(t, []string{"--settings", `{"security":{"folderTrust":{"enabled":false}}}`}, qwen.TrustArgs)
	require.Empty(t, qwen.StatusEnv, "qwen reports once rex answers its query")
	require.Equal(t, []string{"--list-models"}, qwen.ModelsCmd.Args)
	require.Equal(t, "--reasoning-effort={value}", qwen.ModelEffort.ArgTemplate)

	grok := get("grok")
	require.Equal(t, StatusOSC7501, grok.Detect.Status)
	require.Equal(t, []string{"{prompt}"}, grok.PromptArgs)
	require.Equal(t, []string{"--always-approve"}, grok.SkipPermissionsArgs)
	require.Empty(t, grok.TrustArgs)
	require.Nil(t, grok.TrustPrompt)
	require.Equal(t, []string{"models"}, grok.ModelsCmd.Args)
	require.NotEmpty(t, grok.LoginRegex)
	require.Equal(t, "--reasoning-effort={value}", grok.ModelEffort.ArgTemplate)

	cursor := get("cursor")
	require.Equal(t, []string{"cursor-agent"}, cursor.Command)
	require.Empty(t, cursor.Detect.Status, "cursor has no OSC 7501")
	require.Equal(t, []string{"{prompt}"}, cursor.PromptArgs)
	require.Equal(t, []string{"--yolo"}, cursor.SkipPermissionsArgs)
	require.Empty(t, cursor.TrustArgs)
	require.Nil(t, cursor.TrustPrompt)
	require.Nil(t, cursor.ModelEffort, "cursor encodes effort in the model id")
	require.NotEmpty(t, cursor.LoginRegex)
	for _, m := range cursor.Models {
		require.Nil(t, m.Effort)
	}
}
