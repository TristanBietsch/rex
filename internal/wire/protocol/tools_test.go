package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshot_ToolsAdditive(t *testing.T) {
	b, err := json.Marshal(Snapshot{Sessions: []SessionSummary{}})
	require.NoError(t, err)
	require.NotContains(t, string(b), "tools", "omitted when the daemon sends none")

	in := Snapshot{Tools: []ToolInfo{{
		ID: "qwen", Installed: true,
		Models: []ModelInfo{{ID: "qfmodel", Name: "qfmodel", Detected: true}, {ID: "default", Name: "Default model"}},
	}, {ID: "grok"}}}
	b, err = json.Marshal(in)
	require.NoError(t, err)
	require.Contains(t, string(b), `"tools":[{"id":"qwen","installed":true,"models":[{"id":"qfmodel","name":"qfmodel","detected":true},{"id":"default","name":"Default model"}]},{"id":"grok","installed":false}]`)

	var back Snapshot
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, in.Tools, back.Tools)

	// A snapshot from an older daemon has no tools: a client must treat
	// every tool as installed.
	var old Snapshot
	require.NoError(t, json.Unmarshal([]byte(`{"sessions":[],"filter":"all"}`), &old))
	require.Nil(t, old.Tools)
}
