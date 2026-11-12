package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// iconModel is layoutModel with row_icons on, loops on and the extra
// settings in kv (id, value pairs).
func iconLayoutModel(t *testing.T, now time.Time, width int, sessions []protocol.SessionSummary, kv ...any) Model {
	t.Helper()
	m := layoutModel(now, width, sessions...)
	set := append([]any{"row_icons", true, "loops", true}, kv...)
	for i := 0; i+1 < len(set); i += 2 {
		require.NoError(t, m.Store.Set(set[i].(string), set[i+1]))
	}
	return m
}

// iconSessions is a synthetic mix for the strip: a question, a review, a
// subagent fan-out with spend and a long run, a CJK name and a bare row.
func iconSessions(now time.Time) []protocol.SessionSummary {
	cheap, dear := 0.42, 1234.0
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Effort: "high", Tokens: 12345,
			Slug: "dark-mode", State: protocol.StateNeedsInput, WaitReason: protocol.WaitInput,
			CostUSD: &cheap, LastEventAt: now.Add(-4 * time.Minute)},
		{ID: "2", ShortID: "b203", ToolID: "claude", ModelID: "opus", Slug: "release-notes",
			State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview, LastEventAt: now.Add(-11 * time.Minute)},
		{ID: "3", ShortID: "c4d1", ToolID: "claude", ModelID: "opus", Effort: "max", Tokens: 310000,
			Slug: "payment-migration-v2-with-a-very-long-name", Subagents: 12, CostUSD: &dear,
			State: protocol.StateWorking, WorkingSince: now.Add(-45 * time.Minute), LastEventAt: now.Add(-time.Minute)},
		{ID: "4", ShortID: "3c8a", ToolID: "claude", ModelID: "sonnet",
			Slug: "perf-audit", Name: "日本語の長いタスク名前です", State: protocol.StateWorking, LastEventAt: now.Add(-7 * time.Second)},
		{ID: "5", ShortID: "aa01", ToolID: "codex", Slug: "bare", State: protocol.StateFailed},
	}
}

var iconNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestIconLayout_ClassWidths(t *testing.T) {
	cases := []struct {
		w              int
		simple, detail int
	}{{80, 20, 20}, {120, 24, 18}, {150, 32, 24}}
	for _, tc := range cases {
		for _, detailed := range []bool{false, true} {
			m := iconLayoutModel(t, iconNow, tc.w, iconSessions(iconNow))
			m.Detailed = detailed
			c := boardLayout(m, tc.w)
			want := tc.simple
			if detailed {
				want = tc.detail
			}
			require.True(t, c.icons, "width %d", tc.w)
			require.Equal(t, want, c.slug, "width %d detailed=%v", tc.w, detailed)
			for s := slotUnseen; s <= slotLongRun; s++ {
				require.Equal(t, slotWidth[s], c.slots[s], "width %d detailed=%v slot %s", tc.w, detailed, s.id())
			}
			require.Zero(t, c.desc)
			require.Zero(t, c.agents)
			require.Zero(t, c.run)
			require.Equal(t, tc.w, c.used()+c.spacer, "width %d detailed=%v", tc.w, detailed)
		}
	}
}

func TestIconLayout_RowWidthExact(t *testing.T) {
	for _, layout := range []string{"fixed", "fit"} {
		for _, detailed := range []bool{false, true} {
			for w := 40; w <= 200; w++ {
				m := iconLayoutModel(t, iconNow, w, iconSessions(iconNow), "column_layout", layout, "mobile_layout", "off")
				m.Detailed = detailed
				c := boardLayout(m, w)
				require.True(t, c.icons)
				for _, s := range m.Sessions {
					row := renderRow(m, s, c)
					require.NotContains(t, row, "\n")
					require.Equal(t, w, ansi.StringWidth(row), "%s detailed=%v width %d row %s", layout, detailed, w, s.ID)
				}
			}
		}
	}
}

// slotOffsets is each present slot's start column in a row.
func slotOffsets(c rowCols) map[iconSlot]int {
	off := rowIndent + colMarker + colGap + c.slug
	if c.id > 0 {
		off += c.id + colGap
	}
	out := map[iconSlot]int{}
	for s := slotUnseen; s <= slotLongRun; s++ {
		if c.slots[s] > 0 {
			off += colGap
			out[s] = off
			off += c.slots[s]
		}
	}
	return out
}

func TestIconLayout_StableOffsets(t *testing.T) {
	cost := 1234.0
	a := []protocol.SessionSummary{
		{ID: "a", ShortID: "aaaa", ToolID: "claude", Slug: "a-very-long-session-name-that-is-cut-short-here",
			State: protocol.StateWorking, LastEventAt: iconNow.Add(-time.Minute)},
	}
	b := []protocol.SessionSummary{
		{ID: "b", ShortID: "bbbb", ToolID: "claude", Slug: "x", Name: "日本語", Subagents: 12, CostUSD: &cost,
			State: protocol.StateWorking, WorkingSince: iconNow.Add(-2 * time.Hour), LastEventAt: iconNow.Add(-time.Minute)},
	}
	for _, w := range []int{80, 120, 150} {
		for _, detailed := range []bool{false, true} {
			ma := iconLayoutModel(t, iconNow, w, a)
			mb := iconLayoutModel(t, iconNow, w, b)
			ma.Detailed, mb.Detailed = detailed, detailed
			ca, cb := boardLayout(ma, w), boardLayout(mb, w)
			require.Equal(t, slotOffsets(ca), slotOffsets(cb), "width %d detailed=%v", w, detailed)
			for _, mc := range []struct {
				m Model
				c rowCols
			}{{ma, ca}, {mb, cb}} {
				s := mc.m.Sessions[0]
				plain := ansi.Strip(renderRow(mc.m, s, mc.c))
				for slot, off := range slotOffsets(mc.c) {
					sw := mc.c.slots[slot]
					want := ansi.Strip(slotCell(mc.m, s, slot, sw, mc.m.SelectedID == s.ID))
					require.Equal(t, want, ansi.Cut(plain, off, off+sw), "width %d slot %s row %s", w, slot.id(), s.ID)
				}
			}
			rowB := ansi.Strip(renderRow(mb, b[0], cb))
			offs := slotOffsets(cb)
			require.Equal(t, "⋔ 12", ansi.Cut(rowB, offs[slotSubagents], offs[slotSubagents]+4))
			require.Equal(t, " $1.2k", ansi.Cut(rowB, offs[slotSpend], offs[slotSpend]+6))
		}
	}
}

func TestIconLayout_SqueezeOrder(t *testing.T) {
	type event struct {
		name string
		hit  func(c rowCols) bool
	}
	events := []event{
		{"spacer 0", func(c rowCols) bool { return c.spacer == 0 }},
		{"spend gone", func(c rowCols) bool { return c.slots[slotSpend] == 0 }},
		{"long-run gone", func(c rowCols) bool { return c.slots[slotLongRun] == 0 }},
		{"loop gone", func(c rowCols) bool { return c.slots[slotLoop] == 0 }},
		{"subagents gone", func(c rowCols) bool { return c.slots[slotSubagents] == 0 }},
		{"reason 1", func(c rowCols) bool { return c.slots[slotReason] == 1 }},
		{"name shrinks", func(c rowCols) bool { return c.slug < 20 }},
		{"name 12", func(c rowCols) bool { return c.slug == slugMin }},
		{"unseen gone", func(c rowCols) bool { return c.slots[slotUnseen] == 0 }},
	}
	first := make([]int, len(events))
	for w := 100; w >= 20; w-- {
		m := iconLayoutModel(t, iconNow, w, iconSessions(iconNow), "mobile_layout", "off")
		m.Detailed = false
		c := boardLayout(m, w)
		require.True(t, c.icons)
		if w >= 40 {
			require.Equal(t, w, c.used()+c.spacer, "width %d", w)
		}
		for i, e := range events {
			if first[i] == 0 && e.hit(c) {
				first[i] = w
			}
		}
		// A slot once dropped stays dropped at narrower widths.
		if first[1] > 0 && first[1] > w {
			require.Zero(t, c.slots[slotSpend], "width %d", w)
		}
	}
	prevName, prevW := "", 0
	for i, e := range events {
		require.Positive(t, first[i], "%s never happened", e.name)
		if i > 0 {
			require.Greater(t, prevW, first[i], "%s before %s", prevName, e.name)
		}
		prevName, prevW = e.name, first[i]
	}
	// The brief's range: 61 down to 40 still fits a row exactly.
	for w := 61; w >= 40; w-- {
		m := iconLayoutModel(t, iconNow, w, iconSessions(iconNow), "mobile_layout", "off")
		c := boardLayout(m, w)
		for _, s := range m.Sessions {
			require.Equal(t, w, ansi.StringWidth(renderRow(m, s, c)), "width %d", w)
		}
	}
}

func TestIconLayout_FitHidesEmptySlots(t *testing.T) {
	sessions := iconSessions(iconNow)
	for i := range sessions {
		sessions[i].CostUSD = nil
	}
	m := iconLayoutModel(t, iconNow, 120, sessions, "column_layout", "fit")
	c := boardLayout(m, 120)
	require.Zero(t, c.slots[slotSpend], "no cost anywhere: no spend slot")
	require.Zero(t, c.slots[slotUnseen], "nothing unseen: no unseen slot")
	require.Positive(t, c.slots[slotReason])
	require.Positive(t, c.slots[slotSubagents])
	require.Equal(t, 120, c.used()+c.spacer)

	cost := 0.5
	sessions[0].CostUSD = &cost
	m = iconLayoutModel(t, iconNow, 120, sessions, "column_layout", "fit")
	with := boardLayout(m, 120)
	require.Equal(t, slotWidth[slotSpend], with.slots[slotSpend])
	require.Equal(t, c.spacer-colGap-slotWidth[slotSpend], with.spacer, "the slot takes its gap with it")
}

func TestIconLayout_NoTextOnRow(t *testing.T) {
	s := protocol.SessionSummary{ID: "t", ShortID: "t001", ToolID: "claude", Slug: "auth",
		Title: "ship the auth fix", Description: "rewrote the token refresh",
		State: protocol.StateWorking, LastEventAt: iconNow.Add(-time.Minute)}
	for _, detailed := range []bool{false, true} {
		m := iconLayoutModel(t, iconNow, 120, []protocol.SessionSummary{s}, "summary_preview", false)
		m.Detailed = detailed
		out := ansi.Strip(renderBoard(m, 120, 0))
		require.NotContains(t, out, "ship the auth fix", "detailed=%v", detailed)
		require.NotContains(t, out, "rewrote the token refresh", "detailed=%v", detailed)
	}
}

func TestIconLayout_OffIsOldPath(t *testing.T) {
	s := protocol.SessionSummary{ID: "t", ShortID: "t001", ToolID: "claude", Slug: "auth",
		Title: "ship the auth fix", State: protocol.StateWorking, LastEventAt: iconNow.Add(-time.Minute)}
	m := iconLayoutModel(t, iconNow, 120, []protocol.SessionSummary{s}, "row_icons", false)
	m.Detailed = false
	o := layoutOptsOf(m)
	require.Nil(t, o.icons)
	c := boardLayout(m, 120)
	require.False(t, c.icons)
	require.Equal(t, [6]int{}, c.slots)
	require.Positive(t, c.desc)
	off := renderBoard(m, 120, 0)
	require.Contains(t, ansi.Strip(off), "ship the auth fix")

	// The off path is the text-column renderer: same output as a layout
	// sized with no icons at all.
	want := []string{}
	for _, line := range strings.Split(off, "\n") {
		if strings.Contains(ansi.Strip(line), "t001") || strings.Contains(ansi.Strip(line), "auth") {
			want = append(want, line)
		}
	}
	require.Contains(t, want, renderRow(m, s, layoutRows(m.Sessions, m.now(), 120, layoutOpts{times: o.times, resume: o.resume})))

	on := iconLayoutModel(t, iconNow, 120, []protocol.SessionSummary{s})
	on.Detailed = false
	require.NotEqual(t, off, renderBoard(on, 120, 0))
}
