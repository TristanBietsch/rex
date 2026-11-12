package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Before the daemon sends its tool list (or against one that predates it)
// the chips are the enabled built-in agents in registry order, less the
// extra agents: those show only once the daemon reports them installed.
func TestFilterTools_FallbackIsEnabledBuiltins(t *testing.T) {
	m := goldenBoard(t, goldenSize{120, 40}, false)
	require.Equal(t, []string{"all", "claude", "codex", "antigravity", "ollama"}, filterTools(m))
}

// With the daemon's list known, the chips are the installed agents: not the
// echo test tool, not an opt-in built-in (gemini), and a user's own tool too.
func TestFilterTools_DaemonInstalled(t *testing.T) {
	m := pickerModel(t, "claude", "cursor", "grok", "echo", "gemini")
	m.DaemonTools = append(m.DaemonTools, protocol.ToolInfo{ID: "mytool", Installed: true})
	require.Equal(t, []string{"all", "claude", "cursor", "grok", "mytool"}, filterTools(m))

	require.NoError(t, m.Store.Set("extra_agents", false))
	require.Equal(t, []string{"all", "claude", "mytool"}, filterTools(m))
}

// t cycles through the extra agents like any other chip, and :filter takes
// them even with no session on the board.
func TestFilterTools_ExtraAgentsFilterable(t *testing.T) {
	m := pickerModel(t, "claude", "qwen", "cursor", "grok")
	var seen []string
	for range filterTools(m) {
		m = cycleFilter(m)
		seen = append(seen, m.Filter)
	}
	require.Equal(t, []string{"claude", "qwen", "cursor", "grok", "all"}, seen)

	for _, id := range []string{"qwen", "cursor", "grok"} {
		got, _ := executeCommand(m, "filter "+id)
		require.Equal(t, id, got.Filter)
		require.Empty(t, got.Err)
	}
}
