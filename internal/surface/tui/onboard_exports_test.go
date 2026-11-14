package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// KnownAgents is the wizard's list: no echo, no opt-in tools.
func TestKnownAgents(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	var ids []string
	for _, a := range KnownAgents(reg) {
		ids = append(ids, a.ID)
	}
	require.NotContains(t, ids, "echo")
	require.NotContains(t, ids, "gemini")
	require.Contains(t, ids, "claude")
	require.Contains(t, ids, "antigravity")
}

func TestAgentCommandAndHint(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	agy, ok := reg.Find("antigravity")
	require.True(t, ok)
	require.Equal(t, "agy", AgentCommand(agy))
	claude, ok := reg.Find("claude")
	require.True(t, ok)
	require.Contains(t, AgentInstallHint(claude), "https://")

	bare := registry.Tool{ID: "x", Command: []string{"xbin"}}
	require.Equal(t, "install xbin and put it on your PATH", AgentInstallHint(bare))
}

func TestBoardKeyDesc(t *testing.T) {
	d, ok := BoardKeyDesc("cc")
	require.True(t, ok)
	require.Equal(t, "mark done / undo", d)
	_, ok = BoardKeyDesc("qq / ctrl+]")
	require.True(t, ok, "attach keys count")
	_, ok = BoardKeyDesc("ctrl+s")
	require.False(t, ok, "a new-agent page key is not a board key")
	_, ok = BoardKeyDesc("nope")
	require.False(t, ok)
}
