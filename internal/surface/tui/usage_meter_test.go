package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func headerTopPlain(m Model, width int) string {
	row, _, _ := headerTopRow(m, width)
	return ansi.Strip(row)
}

func TestUsageMeter_CompactIsTheWorstWindowPerLiveTool(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	got := headerTopPlain(m, 118)
	require.Contains(t, got, "claude ▰▰▱▱▱ 42%", "the worst of 42% and 18%")
	require.Contains(t, got, "codex ▰▰▰▰▱ 72%")
	require.NotContains(t, got, "antigravity", "no live session")
	require.NotContains(t, got, "grok", "no live session")
	require.NotContains(t, got, "wk", "compact has no window labels")
	require.Equal(t, 118, ansi.StringWidth(got))
	require.Contains(t, got, "42% · codex")
	require.Regexp(t, `72%  $`, got, "right margin")
}

func TestUsageMeter_Full(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	require.NoError(t, m.Store.Set("usage_meter", "full"))
	got := headerTopPlain(m, 118)
	require.Contains(t, got, "claude 5h ▰▰▱▱▱ 42% 2h10m wk ▰▱▱▱▱ 18% 4d")
}

func TestUsageMeter_OffAndDefault(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	require.Equal(t, "compact", usageMeterMode(m), "default")
	require.NoError(t, m.Store.Set("usage_meter", "off"))
	require.Equal(t, "off", usageMeterMode(m))
	require.NotContains(t, headerTopPlain(m, 118), "▰")

	m = meterModel(t, goldenSize{120, 40})
	m.Usage = nil
	require.Equal(t, "off", usageMeterMode(m), "an old daemon sends no usage: hidden")
	m.Usage = &protocol.Usage{Enabled: false}
	require.Equal(t, "off", usageMeterMode(m), "the daemon switched usage off")
}

func TestUsageMeter_YieldsToTheCounts(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	bareModel := m
	bareModel.Usage = nil
	for _, w := range []int{100, 80, 75, 60, 48, 40, 30} {
		row := headerTopPlain(m, w)
		bare := trimRight(headerTopPlain(bareModel, w))
		require.LessOrEqual(t, ansi.StringWidth(row), w)
		// The counts are never cut for the meter: whatever the bare row
		// shows is still there, byte for byte.
		require.Equal(t, bare, row[:len(bare)], "width %d", w)
	}

	// One tool still fits where two do not.
	one := headerTopPlain(m, 75)
	require.Contains(t, one, "claude")
	require.NotContains(t, one, "codex")
	// Too short for any: no meter at all.
	require.NotContains(t, headerTopPlain(m, 40), "▰")
}

func trimRight(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}

func TestUsageMeter_StaleReadingIsDim(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	m.Sessions = append(m.Sessions, protocol.SessionSummary{ID: "ag", ToolID: "antigravity", State: protocol.StateWorking})
	row, _, _ := headerTopRow(m, 118)
	require.Contains(t, ansi.Strip(row), "antigravity ▰▰▰▰▰ 91%")
	withProfile(termenv.TrueColor, func() {
		row, _, _ := headerTopRow(m, 118)
		require.Contains(t, row, styleDim.Render("▰▰▰▰▰ 91%"), "stale: dim, not danger")
		require.Contains(t, row, quotaStyle(42, false).Render("▰▰▱▱▱ 42%"))
		require.Contains(t, row, quotaStyle(72, false).Render("▰▰▰▰▱ 72%"))
	})
}

func TestUsageMeter_ClickOpensThePage(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	fu := &fakeUsager{rep: usageReport(goldenNow)}
	m.usager = fu
	m.Width = 120
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	_, lo, hi := headerTopRow(m, contentWidth(120))
	require.Greater(t, hi, lo)
	click := func(x, y int) (Model, tea.Cmd) {
		return updateBoardMouse(m, tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	next, cmd := click(lo-3, 1)
	require.Equal(t, FocusBoard, next.Focus, "left of the meter")
	require.Nil(t, cmd)
	next, _ = click(lo+2, 2)
	require.Equal(t, FocusBoard, next.Focus, "wrong row")
	next, cmd = click(lo+2, 1)
	require.Equal(t, FocusUsage, next.Focus)
	require.NotNil(t, cmd)
	next = deliver(t, next, cmd)
	require.Len(t, fu.calls, 1)
	require.NotNil(t, next.UsagePage.Report)
}
