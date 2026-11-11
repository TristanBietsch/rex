package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterCommandSections(t *testing.T) {
	hidden := func(ids ...string) map[string]bool {
		if len(ids) == 0 {
			return nil
		}
		out := map[string]bool{}
		for _, id := range ids {
			out[id] = true
		}
		return out
	}
	cases := []struct {
		name   string
		setup  string // a command run first
		line   string
		hidden map[string]bool
		filter string
		err    string
	}{
		{name: "hide by id", line: "filter hide done", hidden: hidden("done"), filter: "all"},
		{name: "hide by prefix", line: "filter hide w", hidden: hidden("working"), filter: "all"},
		{name: "hide by number", line: "filter hide 1", hidden: hidden("needs"), filter: "all"},
		{name: "hide by title", line: "filter hide Needs", hidden: hidden("needs"), filter: "all"},
		{name: "hide several", line: "filter hide needs working", hidden: hidden("needs", "working"), filter: "all"},
		{name: "show", setup: "filter hide needs done", line: "filter show done", hidden: hidden("needs"), filter: "all"},
		{name: "show all", setup: "filter hide needs done", line: "filter show all", hidden: nil, filter: "all"},
		{name: "only", line: "filter only working", hidden: hidden("needs", "done"), filter: "all"},
		{name: "only two", line: "filter only needs working", hidden: hidden("done"), filter: "all"},
		{name: "bare clears sections and tool", setup: "filter hide done", line: "filter", hidden: nil, filter: "all"},
		{name: "bare clears tool", setup: "filter claude", line: "filter", hidden: nil, filter: "all"},
		{name: "unknown section", line: "filter hide nope", err: `filter: unknown section "nope"`},
		{name: "hide needs a section", line: "filter hide", err: "filter: hide needs a section"},
		{name: "only needs a section", line: "filter only", err: "filter: only needs a section"},
		{name: "two words", line: "filter claude codex", err: "filter: unknown tool or subcommand claude codex"},
		{name: "hide every section", line: "filter hide needs working done", err: "filter: can't hide every section"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := filterModel()
			if tc.setup != "" {
				m, _ = executeCommand(m, tc.setup)
				require.Empty(t, m.Err)
			}
			m, _ = executeCommand(m, tc.line)
			if tc.err != "" {
				require.Contains(t, m.Err, tc.err)
				return
			}
			require.Empty(t, m.Err)
			require.Equal(t, tc.hidden, m.Hidden)
			require.Equal(t, tc.filter, m.Filter)
		})
	}
}

// `:filter hide done` reseats the selection like the z chords do.
func TestFilterCommandHideReselects(t *testing.T) {
	m := filterModel()
	m.SelectedID = "d1"
	m, _ = executeCommand(m, "filter hide done")
	require.Equal(t, "n1", m.SelectedID)
}
