package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var timesNow = time.Date(2026, 3, 10, 16, 0, 0, 0, time.UTC)

func TestCompletedAt(t *testing.T) {
	start := timesNow.Add(-3 * time.Hour)
	end := timesNow.Add(-90 * time.Minute)
	mark := timesNow.Add(-2 * time.Hour)
	last := timesNow.Add(-80 * time.Minute)
	cases := []struct {
		name string
		s    protocol.SessionSummary
		want time.Time
	}{
		{"live", protocol.SessionSummary{State: protocol.StateWorking, StartedAt: start}, time.Time{}},
		{"exit", protocol.SessionSummary{State: protocol.StateDone, StartedAt: start, EndedAt: end}, end},
		{"marked then exit", protocol.SessionSummary{State: protocol.StateDone, EndedAt: end, RetiredAt: mark}, mark},
		{"exit then mark", protocol.SessionSummary{State: protocol.StateDone, EndedAt: mark, RetiredAt: end}, mark},
		{"marked, still running", protocol.SessionSummary{State: protocol.StateWorking, RetiredAt: mark}, mark},
		{"older daemon falls back to last event", protocol.SessionSummary{State: protocol.StateFailed, LastEventAt: last}, last},
		{"legacy mark without a time", protocol.SessionSummary{State: protocol.StateWorking, RetireAt: timesNow}, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, tc.want.Equal(completedAt(tc.s)), "got %v want %v", completedAt(tc.s), tc.want)
		})
	}
}

func TestClockStamp(t *testing.T) {
	require.Equal(t, "14:02", clockStamp(timesNow, time.Date(2026, 3, 10, 14, 2, 0, 0, time.UTC)))
	require.Equal(t, "Mar 9", clockStamp(timesNow, time.Date(2026, 3, 9, 23, 59, 0, 0, time.UTC)))
	require.Equal(t, "2025", clockStamp(timesNow, time.Date(2025, 12, 31, 12, 0, 0, 0, time.UTC)))
	// The stamp is in now's location.
	loc := time.FixedZone("x", 2*3600)
	require.Equal(t, "18:00", clockStamp(timesNow.In(loc), timesNow))
}

func TestSpanText(t *testing.T) {
	s := protocol.SessionSummary{State: protocol.StateDone,
		StartedAt: time.Date(2026, 3, 10, 14, 2, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)}
	require.Equal(t, "14:02→15:30", spanText(timesNow, s))
	s.State, s.EndedAt = protocol.StateWorking, time.Time{}
	require.Equal(t, "14:02→", spanText(timesNow, s))
	require.Equal(t, "", spanText(timesNow, protocol.SessionSummary{}), "no start, no span")
}

func TestApplyPatch_EndedAt(t *testing.T) {
	var s protocol.SessionSummary
	applyPatch(&s, map[string]any{"ended_at": "2026-03-10T15:30:00Z"})
	require.Equal(t, time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC), s.EndedAt.UTC())
	applyPatch(&s, map[string]any{"ended_at": nil})
	require.True(t, s.EndedAt.IsZero(), "null clears it")
}

func spanSessions() []protocol.SessionSummary {
	d := func(h, m int) time.Time { return time.Date(2026, 3, 10, h, m, 0, 0, time.UTC) }
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Tokens: 12345, Slug: "dark-mode",
			Description: "system theme vs explicit toggle", State: protocol.StateWorking,
			StartedAt: d(14, 2), LastEventAt: d(15, 55)},
		{ID: "2", ShortID: "b203", ToolID: "claude", ModelID: "sonnet", Tokens: 900, Slug: "release-notes",
			Description: "draft ready", State: protocol.StateDone,
			StartedAt: d(9, 0), EndedAt: d(9, 40), LastEventAt: d(9, 40)},
	}
}

func spanModel(width int, detailed bool) Model {
	m := layoutModel(timesNow, width, spanSessions()...)
	m.Detailed = detailed
	return m
}

func TestBoardSpanColumn(t *testing.T) {
	m := spanModel(160, true)
	rows := boardRows(t, m, 160)
	require.Contains(t, rows["1"], "14:02→")
	require.Contains(t, rows["2"], "09:00→09:40")
	// After tokens, before the age.
	require.Less(t, strings.Index(rows["2"], "900 tk"), strings.Index(rows["2"], "09:00→09:40"))

	// The simple view has no timestamps.
	simple := spanModel(160, false)
	out := ansi.Strip(renderBoard(simple, 160, 0))
	require.NotContains(t, out, "→")
}

// The span is the first column to go on a narrow terminal: tokens survive it.
func TestBoardLayout_SpanDropsBeforeTokens(t *testing.T) {
	sessions := spanSessions()
	full := layoutRows(sessions, timesNow, 160, true)
	require.Positive(t, full.span)
	require.Positive(t, full.tokens)

	// Narrow enough that something has to give, but tokens can still stay.
	for w := 160; w > 40; w-- {
		c := layoutRows(sessions, timesNow, w, true)
		if c.span == 0 {
			require.Positive(t, c.tokens, "width %d: span dropped first, tokens still there", w)
			return
		}
	}
	t.Fatal("span never dropped")
}

func TestBoardLayout_SpanRowsFillWidthExactly(t *testing.T) {
	for _, w := range layoutWidths {
		m := spanModel(w, true)
		cols := boardLayout(m, w)
		for _, s := range m.Sessions {
			require.Equal(t, w, ansi.StringWidth(renderRow(m, s, cols)), "width %d row %s", w, s.ID)
		}
	}
}
