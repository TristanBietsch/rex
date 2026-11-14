package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// filterModel has one session per section and two tools, selection on the
// Done row so a tool filter that hides it must move the selection.
func filterModel() Model {
	m := boardModel(
		protocol.SessionSummary{ID: "n1", Slug: "n1", ToolID: "claude", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "w1", Slug: "w1", ToolID: "antigravity", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "d1", Slug: "d1", ToolID: "claude", State: protocol.StateDone},
		protocol.SessionSummary{ID: "x1", Slug: "x1", ToolID: "mytool", State: protocol.StateWorking},
	)
	m.SelectedID = "w1"
	return m
}

func TestFilterCommand_ToolReselects(t *testing.T) {
	m, _ := executeCommand(filterModel(), "filter claude")
	require.Equal(t, "claude", m.Filter)
	require.Empty(t, m.Err)
	require.Equal(t, "n1", m.SelectedID, "the antigravity row is hidden: selection moves to the first visible row")
	require.GreaterOrEqual(t, indexOfSelected(m), 0)
}

func TestFilterCommand_ToolValidation(t *testing.T) {
	cases := []struct {
		arg    string
		ok     bool
		filter string
	}{
		{"claude", true, "claude"},
		{"all", true, "all"},
		{"ollama", true, "ollama"}, // in filterTools, even with no session
		{"mytool", true, "mytool"}, // on the board, not in filterTools
		{"nosuch", false, "all"},   // unknown: filter unchanged
		{"clau", false, "all"},     // no prefix matching for tools
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			m, _ := executeCommand(filterModel(), "filter "+tc.arg)
			require.Equal(t, tc.filter, m.Filter)
			if tc.ok {
				require.Empty(t, m.Err)
			} else {
				require.Contains(t, m.Err, "filter: unknown tool or subcommand "+tc.arg)
			}
		})
	}
}

func TestFilterCommand_ToolEmptiesBoardClearsSelection(t *testing.T) {
	m, _ := executeCommand(filterModel(), "filter ollama")
	require.Equal(t, "ollama", m.Filter)
	require.Empty(t, m.SelectedID)
}
