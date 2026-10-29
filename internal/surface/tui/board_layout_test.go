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

// layoutSessions is a realistic mix: long model ids, effort, tokens, CJK
// name, long slug, a long-run badge on some rows only, a retire label, and a
// row with no model / no time.
func layoutSessions(now time.Time) []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Effort: "max", Tokens: 12345,
			Slug: "dark-mode", Description: "system theme vs explicit toggle — your call",
			State: protocol.StateNeedsInput, LastEventAt: now.Add(-4 * time.Minute)},
		{ID: "2", ShortID: "b203", ToolID: "codex", ModelID: "gpt-5-codex", Effort: "medium", Tokens: 123456,
			Slug: "release-notes", Description: "draft ready — which feature leads?",
			State: protocol.StateWorking, WorkingSince: now.Add(-45 * time.Minute), LastEventAt: now.Add(-11 * time.Minute)},
		{ID: "3", ShortID: "3c8a", ToolID: "claude", ModelID: "sonnet",
			Slug: "perf-audit", Name: "日本語の長いタスク名前です", Description: "events_org_ts index live — p95 38ms",
			State: protocol.StateWorking, LastEventAt: now.Add(-7 * time.Second)},
		{ID: "4", ShortID: "9e51", ToolID: "ollama", ModelID: "qwen2.5-coder:32b-instruct", Tokens: 900,
			Slug: "payment-migration-very-long-slug-name", Description: "porting billing to the new processor — 12/14",
			State: protocol.StateWorking, WorkingSince: now.Add(-7 * time.Hour), LastEventAt: now.Add(-2 * time.Hour)},
		{ID: "5", ShortID: "2b7e", ToolID: "claude", ModelID: "haiku",
			Slug: "test-coverage", Description: "billing/ from 61% → 92% — PR #408 merged",
			State: protocol.StateDone, RetireAt: now.Add(4 * time.Minute), RetiredAt: now.Add(-3 * time.Hour), LastEventAt: now.Add(-9 * 24 * time.Hour)},
		{ID: "6", ShortID: "aa01", ToolID: "claude", Slug: "no-model", Title: "boot", State: protocol.StateFailed},
	}
}

func layoutModel(now time.Time, width int, sessions ...protocol.SessionSummary) Model {
	m := Model{Sessions: sessions, Filter: "all", SelectedID: "2", Width: width, Height: 30,
		Focus: FocusBoard, Store: settings.NewStore()}
	m.Clock = func() time.Time { return now }
	return m
}

// boardRows renders the whole board at width and returns the ansi-stripped
// session rows, keyed by session ID.
func boardRows(t *testing.T, m Model, width int) map[string]string {
	t.Helper()
	out := renderBoard(m, width, 0)
	rows := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		plain := ansi.Strip(line)
		for _, s := range m.Sessions {
			if len(plain) > rowIndent+colMarker+colGap+colID &&
				strings.Contains(plain, "  "+s.ShortID+"  ") {
				rows[s.ID] = plain
			}
		}
	}
	require.Len(t, rows, len(m.Sessions), "every session renders one row")
	return rows
}

var layoutWidths = []int{40, 60, 80, 100, 120, 160, 220}

// Every row is exactly the board width (never wraps, never overflows) and the
// time column sits flush right on every row.
func TestBoardLayout_RowsFillWidthExactly(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, w := range layoutWidths {
		m := layoutModel(now, w, layoutSessions(now)...)
		cols := boardLayout(m, w)
		for _, s := range m.Sessions {
			row := renderRow(m, s, cols)
			require.NotContains(t, row, "\n", "width %d", w)
			require.Equal(t, w, ansi.StringWidth(row), "width %d row %s", w, s.ID)
			plain := ansi.Strip(row)
			tail := strings.TrimSpace(string([]rune(plain)[len([]rune(plain))-colTime:]))
			require.Equal(t, durationAgoAt(now, s.LastEventAt), tail, "time flush right, width %d row %s", w, s.ID)
		}
	}
}

// The full screen never has a board line wider than the terminal.
func TestBoardLayout_ViewNeverOverflows(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, w := range layoutWidths {
		m := layoutModel(now, w, layoutSessions(now)...)
		for i, line := range strings.Split(renderBoard(m, w, 0), "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), w, "width %d line %d: %q", w, i, ansi.Strip(line))
		}
	}
}

// Each info column starts at the same x on every row that has content in it.
func TestBoardLayout_ColumnsAlignVertically(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 160, layoutSessions(now)...)
	rows := boardRows(t, m, 160)
	col := func(row, needle string) int {
		i := strings.Index(row, needle)
		require.GreaterOrEqual(t, i, 0, "%q in %q", needle, row)
		return ansi.StringWidth(row[:i])
	}
	// Model column: same x across rows.
	modelX := col(rows["1"], "opus · max")
	require.Equal(t, modelX, col(rows["2"], "gpt-5-codex · medium"))
	require.Equal(t, modelX, col(rows["3"], "sonnet"))
	require.Equal(t, modelX, col(rows["5"], "haiku"))
	// Tokens column is right-aligned: the "tk" suffix ends at the same x.
	tkEnd := col(rows["1"], "12.3K tk") + len("12.3K tk")
	require.Equal(t, tkEnd, col(rows["2"], "123K tk")+len("123K tk"))
	require.Equal(t, tkEnd, col(rows["4"], "900 tk")+len("900 tk"))
	// Badge column.
	require.Equal(t, col(rows["2"], "▴45m"), col(rows["4"], "▴7h"))
	// Name and description columns.
	require.Equal(t, col(rows["1"], "dark-mode"), col(rows["2"], "release-notes"))
	require.Equal(t, col(rows["1"], "system theme"), col(rows["2"], "draft ready"))
}

// Wide terminals show every info field in full.
func TestBoardLayout_WideShowsEverything(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rows := boardRows(t, layoutModel(now, 160, layoutSessions(now)...), 160)
	require.Contains(t, rows["1"], "opus · max")
	require.Contains(t, rows["1"], "12.3K tk")
	require.Contains(t, rows["2"], "gpt-5-codex · medium")
	require.Contains(t, rows["2"], "123K tk")
	require.Contains(t, rows["2"], "▴45m")
	require.Contains(t, rows["2"], "11m")
	require.Contains(t, rows["3"], "日本語")
	require.Contains(t, rows["4"], "▴7h")
	require.NotContains(t, rows["5"], "done")
	require.NotContains(t, rows["5"], "clears")
	require.True(t, strings.HasSuffix(rows["6"], "-"), "zero time renders as -")
}

// At 80 columns the description keeps a usable width, model+effort stays
// readable, and the time is never cut.
func TestBoardLayout_EightyColumns(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 80, layoutSessions(now)...)
	cols := boardLayout(m, 80)
	require.GreaterOrEqual(t, cols.desc, descMin)
	rows := boardRows(t, m, 80)
	require.Contains(t, rows["1"], "opus · max")
	require.Contains(t, rows["2"], "▴45m")
	require.True(t, strings.HasSuffix(rows["2"], "11m"))
}

// No blank badge column is reserved when no row has a badge, and no tokens
// column when no row has tokens.
func TestBoardLayout_OptionalColumnsCollapse(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	plain := []protocol.SessionSummary{
		{ID: "a", ShortID: "aaaa", ModelID: "opus", Effort: "high", Slug: "alpha", Description: "doing a thing",
			State: protocol.StateWorking, LastEventAt: now.Add(-4 * time.Minute)},
		{ID: "b", ShortID: "bbbb", ModelID: "sonnet", Slug: "beta", Description: "other",
			State: protocol.StateDone, LastEventAt: now.Add(-2 * time.Minute)},
	}
	m := layoutModel(now, 120, plain...)
	cols := boardLayout(m, 120)
	require.Zero(t, cols.run)
	require.Zero(t, cols.tokens)
	require.Equal(t, ansi.StringWidth("opus · high"), cols.model, "model column sized to content")
	require.Equal(t, len("alpha"), cols.slug, "name column sized to content")
	row := ansi.Strip(renderRow(m, plain[0], cols))
	require.True(t, strings.HasSuffix(row, "opus · high  "+"  4m"), "model sits one gap before the time: %q", row)
}

// Narrow widths degrade in order: tokens go before model, model before the
// description drops under its minimum.
func TestBoardLayout_NarrowDegradation(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	sessions := layoutSessions(now)
	prev := boardLayout(layoutModel(now, 220, sessions...), 220)
	require.NotZero(t, prev.tokens)
	require.NotZero(t, prev.model)
	for w := 219; w >= 40; w-- {
		c := boardLayout(layoutModel(now, w, sessions...), w)
		if c.model > 0 {
			require.GreaterOrEqual(t, c.desc, descMin, "width %d: model kept while desc under min", w)
		}
		if c.tokens > 0 {
			require.NotZero(t, c.model, "width %d: tokens kept after model dropped", w)
		}
		require.LessOrEqual(t, c.model, prev.model, "width %d: model grew while narrowing", w)
		require.LessOrEqual(t, c.tokens, prev.tokens, "width %d", w)
		prev = c
	}
}

func TestBoardLayout_NoneAlignsWithIDColumn(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 120, layoutSessions(now)[0])
	var noneLine, rowLine string
	for _, line := range strings.Split(ansi.Strip(renderBoard(m, 120, 0)), "\n") {
		if strings.Contains(line, "(none)") && noneLine == "" {
			noneLine = line
		}
		if strings.Contains(line, "7d4f") {
			rowLine = line
		}
	}
	x := func(line, needle string) int {
		i := strings.Index(line, needle)
		require.GreaterOrEqual(t, i, 0, "%q not found in %q", needle, line)
		return ansi.StringWidth(line[:i])
	}
	require.Equal(t, x(rowLine, "7d4f"), x(noneLine, "(none)"))
}
