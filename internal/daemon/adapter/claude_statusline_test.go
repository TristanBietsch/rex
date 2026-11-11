package adapter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func settingsOf(t *testing.T, args []string) map[string]json.RawMessage {
	t.Helper()
	require.Len(t, args, 2)
	require.Equal(t, "--settings", args[0])
	var cfg map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(args[1]), &cfg))
	return cfg
}

func TestClaudeSettingsArgs_NoStatusLineIsTheHookArgs(t *testing.T) {
	require.Equal(t, ClaudeHookArgs(), ClaudeSettingsArgs(""))
	require.NotContains(t, settingsOf(t, ClaudeSettingsArgs("")), "statusLine")
}

func TestClaudeSettingsArgs_AddsStatusLineAndKeepsEveryHook(t *testing.T) {
	const cmd = "/opt/rex-daemon statusline-tap --session abc"
	cfg := settingsOf(t, ClaudeSettingsArgs(cmd))

	var sl struct{ Type, Command string }
	require.NoError(t, json.Unmarshal(cfg["statusLine"], &sl))
	require.Equal(t, "command", sl.Type)
	require.Equal(t, cmd, sl.Command)

	// The hooks are byte-identical with and without the tap.
	plain := settingsOf(t, ClaudeHookArgs())
	require.JSONEq(t, string(plain["hooks"]), string(cfg["hooks"]))

	// Spot-check that the lines other features rely on are still written:
	// T3 sid, S4 turn-done and notify, R14 wait+ / wait-, subagents.
	hooks := string(cfg["hooks"])
	for _, want := range []string{
		"sid ", hookTurnDone, hookNotify, hookWaitStart, hookWaitEnd,
		hookAgentStart, hookAgentStop, hookAgentSeen, hookAgentsReset,
		"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
		"Notification", "Stop", "SubagentStart", "SubagentStop",
	} {
		require.Contains(t, hooks, want)
	}
}
