package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// legacyKeep is the pre-rowFilter tool predicate.
func legacyKeep(filter string, s protocol.SessionSummary) bool {
	return filter == "all" || filter == "" || s.ToolID == filter
}

func TestRowFilterToolMatchesLegacy(t *testing.T) {
	sessions := demoSessions(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	tools := append([]string{""}, filterTools...)
	for _, tool := range tools {
		parts := partitionSessions(sessions, rowFilter{tool: tool})
		for gi, g := range boardGroups {
			var want []string
			for _, s := range sessions {
				if legacyKeep(tool, s) && g.Match(s, groupCtx{}) {
					want = append(want, s.ID)
				}
			}
			var got, viaGroup []string
			for _, s := range parts[gi] {
				got = append(got, s.ID)
			}
			for _, s := range filterByGroup(sessions, g, rowFilter{tool: tool}) {
				viaGroup = append(viaGroup, s.ID)
			}
			require.Equal(t, want, got, "tool=%q group=%s", tool, g.ID)
			require.Equal(t, want, viaGroup, "tool=%q group=%s", tool, g.ID)
		}
	}
}

func TestRowFilterOfModel(t *testing.T) {
	m := boardModel()
	m.Filter = "codex"
	f := rowFilterOf(m)
	require.Equal(t, "codex", f.tool)
	require.True(t, f.rowsActive())
	require.False(t, rowFilter{tool: "all"}.rowsActive())
	require.False(t, rowFilter{}.rowsActive())
	require.True(t, f.showsSection(boardGroups[groupDone]))
}
