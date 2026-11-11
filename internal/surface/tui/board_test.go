package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 tk"},
		{200, "200 tk"},
		{999, "999 tk"},
		{1500, "1.5K tk"},
		{12345, "12.3K tk"},
		{99949, "99.9K tk"},
		{99999, "100K tk"},
		{123456, "123K tk"},
		{999499, "999K tk"},
		{999500, "1.0M tk"},
		{1234567, "1.2M tk"},
		{99_949_999, "99.9M tk"},
		{123_456_789, "123M tk"},
	}
	for _, c := range cases {
		got := formatTokens(c.n)
		require.Equal(t, c.want, got, "formatTokens(%d)", c.n)
	}
}

func TestModelLabel_WithTokens(t *testing.T) {
	cases := []struct {
		tokens int64
		effort string
		want   string
	}{
		{0, "", "gpt-5.2"},
		{0, "high", "gpt-5.2 · high"},
		{200, "", "gpt-5.2 · 200 tk"},
		{1500, "low", "gpt-5.2 · low · 1.5K tk"},
		{123456, "", "gpt-5.2 · 123K tk"},
	}
	for _, c := range cases {
		s := protocol.SessionSummary{ModelID: "gpt-5.2", Effort: c.effort, Tokens: c.tokens}
		got := modelLabel(s)
		require.Equal(t, c.want, got, "modelLabel(tokens=%d, effort=%q)", c.tokens, c.effort)
	}
}

func TestFleetColor_Deterministic(t *testing.T) {
	// Same name must always produce the same color.
	for _, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		c1 := fleetColor(name)
		c2 := fleetColor(name)
		require.Equal(t, c1, c2, "fleetColor(%q) not deterministic", name)
	}
}

func TestFleetColor_Distinct(t *testing.T) {
	// 10 distinct names should map to at least 4 distinct colors (palette has 10 entries).
	names := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	seen := map[string]struct{}{}
	for _, n := range names {
		seen[string(fleetColor(n))] = struct{}{}
	}
	require.GreaterOrEqual(t, len(seen), 4, "expected at least 4 distinct fleet colors")
}

func TestRowDescription_SummaryThenTitleNeverLastLine(t *testing.T) {
	s := protocol.SessionSummary{Title: "fix auth bug", LastLine: "✶ esc to interrupt"}
	require.Equal(t, "fix auth bug", rowDescription(s))
	s.Description = "editing auth middleware"
	require.Equal(t, "editing auth middleware", rowDescription(s))
	require.Equal(t, "", rowDescription(protocol.SessionSummary{LastLine: "✶"}))
}

// A session is queued from spawn until its agent starts. No board group
// matched StateQueued, so new (or stuck-before-start) sessions never showed
// up. Queued now lives in Working with its own marker glyph.
func TestBoard_QueuedSessionShowsInWorking(t *testing.T) {
	queued := protocol.SessionSummary{ID: "q", ShortID: "q001", Slug: "fresh", State: protocol.StateQueued}
	m := boardModel(protocol.SessionSummary{ID: "w", ShortID: "w001", Slug: "busy", State: protocol.StateWorking}, queued)
	m.Detailed = true // rows are found by short id

	require.Equal(t, []string{"w", "q"}, sessionIDs(orderedSessions(m)), "queued is navigable")
	group := -1
	for _, sl := range boardSlots(m) {
		if sl.kind == slotRow && sl.sess.ID == "q" {
			group = sl.group
		}
	}
	require.GreaterOrEqual(t, group, 0, "queued row in a board group")
	require.Equal(t, "Working", boardGroups[group].Title)

	var row string
	for _, line := range strings.Split(ansi.Strip(renderBoard(m, 120, 0)), "\n") {
		if strings.Contains(line, "q001") {
			row = line
		}
	}
	require.NotEmpty(t, row, "queued row rendered")
	require.Contains(t, row, queuedGlyph)
	require.Contains(t, ansi.Strip(attachStateChip(m, protocol.StateQueued)), queuedGlyph+" queued")
}

// The header counts every session the stats overlay counts: queued folds
// into "working" (its board column), so the header buckets add up to the
// overlay's total.
func TestHeader_CountsQueuedAsWorking(t *testing.T) {
	m := boardModel(
		protocol.SessionSummary{ID: "1", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "2", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "3", State: protocol.StateQueued},
		protocol.SessionSummary{ID: "4", State: protocol.StateDone},
	)
	require.Contains(t, ansi.Strip(renderHeader(m, 160)), "1 need you · 2 working · 1 done")
	stats := ansi.Strip(renderStatsOverlay(m))
	require.Regexp(t, `queued\s+1`, stats)
	require.Regexp(t, `total\s+4`, stats)
}

func sessionIDs(ss []protocol.SessionSummary) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
