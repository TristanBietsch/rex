package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A glyph never touches the number or duration after it.
func TestIconsAreSpacedFromCounts(t *testing.T) {
	s := protocol.SessionSummary{ID: "a", Slug: "busy", State: protocol.StateWorking, Subagents: 12}
	require.Equal(t, subagentGlyph+" 12", strings.TrimSpace(ansi.Strip(subagentCell(s, colAgents, false))))
	require.Equal(t, colAgents, ansi.StringWidth(subagentCell(s, colAgents, false)))

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.WorkingSince = now.Add(-45 * time.Minute)
	require.Equal(t, "▴ 45m", ansi.Strip(longRunBadge(now, s)))
	s.WorkingSince = now.Add(-10 * time.Hour)
	require.LessOrEqual(t, ansi.StringWidth(longRunBadge(now, s)), colRun)

	st := settings.NewStore()
	require.NoError(t, st.Set("header_style", "glyphs"))
	m := waitingBoard()
	m.Store = st
	m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: "f", State: protocol.StateFailed},
		protocol.SessionSummary{ID: "f2", State: protocol.StateCrashed})
	header := ansi.Strip(renderHeader(m, 200))
	require.Contains(t, header, "◆ 1  ◷ 2  ⟳ 2  ● 1")
	require.Contains(t, header, "✕ 2")
}
