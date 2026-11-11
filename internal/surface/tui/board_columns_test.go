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

func TestClassForWidthBreakpoints(t *testing.T) {
	for _, c := range []struct {
		w    int
		want layoutClass
	}{
		{1, classMobile}, {30, classMobile}, {60, classMobile},
		{61, classNarrow}, {109, classNarrow},
		{110, classNormal}, {149, classNormal},
		{150, classWide}, {220, classWide},
	} {
		require.Equal(t, c.want, classForWidth(c.w), "width %d", c.w)
	}
}

// fitModel switches m to column_layout: fit (today's content-sized columns).
func fitModel(t *testing.T, m Model) Model {
	t.Helper()
	if m.Store == nil {
		m.Store = settings.NewStore()
	}
	require.NoError(t, m.Store.Set("column_layout", "fit"))
	return m
}

// A fixed layout never reads the rows: a board with every optional field and
// one with a single bare row size alike.
func TestFixedColumnsIgnoreContent(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	quiet := []protocol.SessionSummary{
		{ID: "a", ShortID: "aaaa", Slug: "a", State: protocol.StateWorking, LastEventAt: now},
	}
	for _, detailed := range []bool{false, true} {
		for _, w := range layoutWidths {
			full := layoutModel(now, w, layoutSessions(now)...)
			full.Detailed = detailed
			empty := layoutModel(now, w, quiet...)
			empty.Detailed = detailed
			require.Equal(t, boardLayout(full, w), boardLayout(empty, w), "detailed=%v width %d", detailed, w)
		}
	}
}

// The sometimes-empty columns (subagents, long-run badge, tokens, span,
// resume) exist in every class that shows them, even on an empty board.
func TestFixedColumnsReservedSometimesEmpty(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		w                                    int
		tokens, span, resume, model, id, run bool
	}{
		{220, true, true, true, true, true, true},
		{150, true, true, true, true, true, true},
		{149, true, false, false, true, true, true},
		{110, true, false, false, true, true, true},
		{109, false, false, false, false, false, true},
		{80, false, false, false, false, false, true},
		{61, false, false, false, false, false, true},
	} {
		cols := boardLayout(layoutModel(now, c.w), c.w) // no sessions at all
		require.Equal(t, c.tokens, cols.tokens > 0, "tokens width %d", c.w)
		require.Equal(t, c.span, cols.span > 0, "span width %d", c.w)
		require.Equal(t, c.resume, cols.resume > 0, "resume width %d", c.w)
		require.Equal(t, c.model, cols.model > 0, "model width %d", c.w)
		require.Equal(t, c.id, cols.id > 0, "id width %d", c.w)
		require.Equal(t, c.run, cols.agents > 0 && cols.run > 0, "agents and run width %d", c.w)
		require.GreaterOrEqual(t, cols.desc, 18, "width %d", c.w)
		require.Equal(t, c.w, cols.used()+cols.desc, "width %d", c.w)
	}
}

// The simple view never has the detailed columns, and the resume column
// follows its feature.
func TestFixedColumnsSimpleAndResumeOff(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 220)
	m.Detailed = false
	c := boardLayout(m, 220)
	require.Zero(t, c.id+c.model+c.tokens+c.span+c.resume)
	require.Equal(t, 22, c.slug)

	m.Detailed = true
	m.Store = flagOff(t, "resume")
	require.Zero(t, boardLayout(m, 220).resume)
	require.Positive(t, boardLayout(m, 220).span)
}

// Inside a class every column keeps its width: only the description grows.
func TestFixedColumnsStepOnlyAtBreakpoints(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, detailed := range []bool{false, true} {
		var prev rowCols
		for w := breakNarrow; w <= 260; w++ {
			m := layoutModel(now, w, layoutSessions(now)...)
			m.Detailed = detailed
			c := boardLayout(m, w)
			c.desc = 0
			if w != breakNarrow && w != breakNormal && w != breakWide {
				require.Equal(t, prev, c, "detailed=%v width %d moved columns inside a class", detailed, w)
			}
			prev = c
		}
	}
}

// column_layout: fit keeps today's content-sized layout.
func TestFitModeIsLayoutFit(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sessions := layoutSessions(now)
	for _, detailed := range []bool{false, true} {
		for _, w := range layoutWidths {
			m := fitModel(t, layoutModel(now, w, sessions...))
			m.Detailed = detailed
			require.Equal(t, layoutFit(sessions, now, w, detailed, true), boardLayout(m, w), "detailed=%v width %d", detailed, w)
		}
	}
}

// Both modes and both views: every row is exactly the width, the board never
// overflows, and the age sits flush right.
func TestColumnLayoutsFillWidthExactly(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, fit := range []bool{false, true} {
		for _, detailed := range []bool{false, true} {
			for _, w := range layoutWidths {
				m := layoutModel(now, w, layoutSessions(now)...)
				m.Detailed = detailed
				if fit {
					m = fitModel(t, m)
				}
				cols := boardLayout(m, w)
				for _, s := range m.Sessions {
					row := renderRow(m, s, cols)
					require.Equal(t, w, ansi.StringWidth(row), "fit=%v detailed=%v width %d row %s", fit, detailed, w, s.ID)
					plain := []rune(ansi.Strip(row))
					tail := strings.TrimSpace(string(plain[len(plain)-colTime:]))
					require.Equal(t, durationAgoAt(now, s.LastEventAt), tail, "age flush right, fit=%v width %d", fit, w)
				}
				for i, line := range strings.Split(renderBoard(m, w, 0), "\n") {
					require.LessOrEqual(t, ansi.StringWidth(line), w, "fit=%v detailed=%v width %d line %d", fit, detailed, w, i)
				}
			}
		}
	}
}
