package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var cellNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func cellModel(t *testing.T, kv ...any) Model {
	m := iconModel(t, kv...)
	m.Clock = func() time.Time { return cellNow }
	return m
}

func plainCell(m Model, s protocol.SessionSummary, slot iconSlot, w int) string {
	return ansi.Strip(slotCell(m, s, slot, w, false))
}

func TestReasonCell(t *testing.T) {
	m := cellModel(t, "waiting_section", true)
	ni := protocol.SessionSummary{State: protocol.StateNeedsInput}
	s := ni
	s.WaitReason = protocol.WaitInput
	require.Equal(t, "? asks  ", plainCell(m, s, slotReason, 8))
	s.WaitReason = protocol.WaitReview
	require.Equal(t, "        ", plainCell(m, s, slotReason, 8), "a finished turn shows nothing (2026-11-12)")
	s = ni
	s.LastLine = "Proceed?"
	require.Equal(t, "? asks  ", plainCell(m, s, slotReason, 8))

	w := protocol.SessionSummary{State: protocol.StateWorking, WaitingOn: "shell", WaitingSince: cellNow.Add(-42 * time.Second)}
	require.Equal(t, "sh 42s  ", plainCell(m, w, slotReason, 8))
	w.WaitingOn = "web"
	require.Equal(t, "web 42s ", plainCell(m, w, slotReason, 8))
	w.WaitingOn = "subagent"
	require.Equal(t, "sub 42s ", plainCell(m, w, slotReason, 8))

	off := cellModel(t, "waiting_section", false)
	w.WaitingOn = "shell"
	require.Equal(t, "        ", plainCell(off, w, slotReason, 8))

	md := ni
	md.WaitReason = protocol.WaitInput
	md.RetireAt = cellNow.Add(time.Hour)
	require.Equal(t, "        ", plainCell(m, md, slotReason, 8))

	s = ni
	s.WaitReason = protocol.WaitInput
	require.Equal(t, "?", plainCell(m, s, slotReason, 1))
	s.WaitReason = protocol.WaitReview
	require.Equal(t, " ", plainCell(m, s, slotReason, 1))
	require.Equal(t, " ", plainCell(m, w, slotReason, 1))
}

func TestReasonCell_ScreenOnly(t *testing.T) {
	m := cellModel(t)
	s := protocol.SessionSummary{State: protocol.StateNeedsInput, LastLine: "Working…"}
	require.Equal(t, "        ", plainCell(m, s, slotReason, 8))
}

func TestSpendText(t *testing.T) {
	for in, want := range map[float64]string{
		0.42: "$0.42", 9.999: "$10.0", 12.34: "$12.3", 123.4: "$123", 1234: "$1.2k", 2e6: "$999k",
	} {
		require.Equal(t, want, spendText(in), "%v", in)
	}
	m := cellModel(t)
	c := 1234.0
	require.Equal(t, " $1.2k", plainCell(m, protocol.SessionSummary{CostUSD: &c}, slotSpend, 6))
}

func TestSpendCell_NilBlank(t *testing.T) {
	m := cellModel(t)
	require.Equal(t, "      ", plainCell(m, protocol.SessionSummary{}, slotSpend, 6))
	z := 0.0
	require.Equal(t, " $0.00", plainCell(m, protocol.SessionSummary{CostUSD: &z}, slotSpend, 6))
}

func TestSpendStyle(t *testing.T) {
	require.Equal(t, styleCyber, spendStyle(4.99))
	require.Equal(t, styleLongRun[1], spendStyle(5))
	require.Equal(t, styleLongRun[0], spendStyle(20))
}

func TestSpendCellStyle(t *testing.T) {
	cost := func(usd float64) protocol.SessionSummary { return protocol.SessionSummary{CostUSD: &usd} }
	done := func(s protocol.SessionSummary) protocol.SessionSummary {
		s.RetireAt = cellNow.Add(time.Hour)
		return s
	}
	require.Equal(t, styleCyber, spendCellStyle(cost(0.42)))
	require.Equal(t, styleDim, spendCellStyle(done(cost(0.42))), "a marked-done row keeps a dim spend")
	require.Equal(t, styleLongRun[1], spendCellStyle(done(cost(6))), "the red tiers still show on a marked-done row")
	require.Equal(t, styleLongRun[0], spendCellStyle(cost(25)))
	require.Equal(t, styleDim, spendCellStyle(protocol.SessionSummary{}), "no cost, nothing to color")
}

// The help legend explains the strip's colors, so its sh and spend samples
// wear the board's colors.
func TestRowIconLegend_ShAndSpendMatchBoard(t *testing.T) {
	m := cellModel(t, "waiting_section", true, "usage", true)
	withProfile(termenv.TrueColor, func() {
		keys := map[string]string{}
		for _, r := range rowIconLegend(m) {
			keys[ansi.Strip(r.Key)] = r.Key
		}
		require.Equal(t, styleCyber.Render("sh 42s"), keys["sh 42s"])
		require.Equal(t, styleCyber.Render("$0.42"), keys["$0.42"])
	})
}

func TestReasonStyle_ShellIsCyber(t *testing.T) {
	m := cellModel(t, "waiting_section", true)
	w := protocol.SessionSummary{State: protocol.StateWorking, WaitingOn: "shell", WaitingSince: cellNow.Add(-42 * time.Second)}
	text, style := reasonText(m, w)
	require.Equal(t, "sh 42s", text)
	require.Equal(t, styleCyber, style)
	require.NotEqual(t, styleWorking, style, "sh and sub no longer share a color")
	for _, kind := range []string{"web", "subagent"} {
		w.WaitingOn = kind
		_, style = reasonText(m, w)
		require.Equal(t, styleWorking, style, kind)
	}
}

func TestLoopSlotText(t *testing.T) {
	m := cellModel(t)
	m.Loops = []protocol.LoopSummary{
		{ID: "l1", State: protocol.LoopRunning, Iteration: 3, MaxIterations: 8},
		{ID: "l2", State: protocol.LoopPaused, Iteration: 3, MaxIterations: 8},
		{ID: "l3", State: protocol.LoopStopped},
	}
	require.Equal(t, "↻ 3/8", loopSlotText(m, protocol.SessionSummary{LoopID: "l1"}))
	require.Equal(t, "↻ paused", loopSlotText(m, protocol.SessionSummary{LoopID: "l2"}))
	require.Equal(t, "↻ stop", loopSlotText(m, protocol.SessionSummary{LoopID: "l3"}))
	require.Equal(t, "↻ 3", loopSlotText(m, protocol.SessionSummary{LoopID: "gone", LoopIter: 3}))
	require.Equal(t, "", loopSlotText(m, protocol.SessionSummary{}))
}

func TestSlotCell_ASCII(t *testing.T) {
	m := cellModel(t, "ascii_glyphs", true)
	m.Loops = []protocol.LoopSummary{{ID: "l1", State: protocol.LoopRunning, Iteration: 3, MaxIterations: 8}}
	s := protocol.SessionSummary{
		State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview,
		LoopID: "l1", Subagents: 3,
	}
	require.Equal(t, "        ", plainCell(m, s, slotReason, 8))
	require.Equal(t, "@ 3/8   ", plainCell(m, s, slotLoop, 8))
	require.Equal(t, "Y 3 ", plainCell(m, s, slotSubagents, 4))
	w := protocol.SessionSummary{State: protocol.StateWorking, WorkingSince: cellNow.Add(-45 * time.Minute)}
	require.Equal(t, "^ 45m", plainCell(m, w, slotLongRun, 5))
	require.Equal(t, "*", ansi.Strip(iconGlyph(m, "unseen")))
}

func TestSlotCell_Width(t *testing.T) {
	m := cellModel(t)
	c := 1.5
	s := protocol.SessionSummary{CostUSD: &c, Subagents: 2}
	for slot := slotUnseen; slot <= slotLongRun; slot++ {
		require.Equal(t, slotWidth[slot], ansi.StringWidth(slotCell(m, s, slot, slotWidth[slot], false)))
	}
}
