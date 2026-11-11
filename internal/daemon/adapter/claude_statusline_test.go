package adapter

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// goldenSettings is the --settings JSON exactly as integration/todo built it
// before the usage tap existed (testdata/claude_settings_integration.json).
// A change to the hooks on purpose regenerates this file; a change by the
// tap must never.
func goldenSettings(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/claude_settings_integration.json")
	require.NoError(t, err)
	return string(b)
}

func settingsArgs(t *testing.T, args []string) string {
	t.Helper()
	require.Len(t, args, 2)
	require.Equal(t, "--settings", args[0])
	return args[1]
}

func TestClaudeSettingsArgs_WithoutStatusLineIsByteIdenticalToTheGolden(t *testing.T) {
	want := goldenSettings(t)
	require.Equal(t, want, settingsArgs(t, ClaudeSettingsArgs("")))
	require.Equal(t, want, settingsArgs(t, ClaudeHookArgs()))
}

func TestClaudeSettingsArgs_StatusLineAddsOnlyThatKey(t *testing.T) {
	const cmd = "/opt/rex-daemon statusline-tap --session abc"
	var got, want map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(settingsArgs(t, ClaudeSettingsArgs(cmd))), &got))
	require.NoError(t, json.Unmarshal([]byte(goldenSettings(t)), &want))

	var sl struct{ Type, Command string }
	require.NoError(t, json.Unmarshal(got["statusLine"], &sl))
	require.Equal(t, "command", sl.Type)
	require.Equal(t, cmd, sl.Command)

	delete(got, "statusLine")
	require.Equal(t, len(want), len(got), "no key other than statusLine was added")
	for k, v := range want {
		require.JSONEq(t, string(v), string(got[k]), "key %q is unchanged", k)
	}
}
