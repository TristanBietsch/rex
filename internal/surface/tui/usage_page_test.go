package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func fptr(v float64) *float64 { return &v }

// usageQuotas is the live quota set the page and the meter tests share: a
// claude with two windows, a grok month window, a codex at warn level, and
// an antigravity reading that went stale at a danger value.
func usageQuotas(now time.Time) []protocol.UsageQuota {
	return []protocol.UsageQuota{
		{Tool: "claude", Window: "5h", UsedPct: fptr(42), ResetsAt: now.Add(2*time.Hour + 10*time.Minute), At: now.Add(-time.Minute)},
		{Tool: "claude", Window: "week", UsedPct: fptr(18), ResetsAt: now.Add(4*24*time.Hour + time.Hour), At: now.Add(-time.Minute)},
		{Tool: "grok", Window: "month", UsedPct: fptr(31), ResetsAt: now.Add(9*24*time.Hour + time.Hour), At: now.Add(-2 * time.Minute)},
		{Tool: "codex", Window: "5h", UsedPct: fptr(72), ResetsAt: now.Add(35 * time.Minute), At: now.Add(-time.Minute)},
		{Tool: "antigravity", Window: "week", UsedPct: fptr(91), ResetsAt: now.Add(3 * 24 * time.Hour), At: now.Add(-40 * time.Minute)},
	}
}

func usageReport(now time.Time) protocol.UsageReport {
	return protocol.UsageReport{
		Range: protocol.UsageRangeToday,
		Since: now.Add(-12 * time.Hour),
		Total: protocol.UsageRow{Sessions: 19, Tokens: 1_340_000, TokensEstimated: true, CostUSD: fptr(19.10)},
		Tools: []protocol.UsageToolRow{
			{Tool: "claude", UsageRow: protocol.UsageRow{Sessions: 14, Tokens: 1_200_000, CostUSD: fptr(18.40)}, Models: []protocol.UsageModelRow{
				{Model: "opus", UsageRow: protocol.UsageRow{Sessions: 3, Tokens: 410_000, CostUSD: fptr(11.90)}},
				{Model: "sonnet", UsageRow: protocol.UsageRow{Sessions: 11, Tokens: 790_000, CostUSD: fptr(6.50)}},
			}},
			{Tool: "grok", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 88_000, TokensEstimated: true, CostUSD: fptr(0.70)}, Models: []protocol.UsageModelRow{
				{Model: "grok-build", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 88_000, TokensEstimated: true, CostUSD: fptr(0.70)}},
			}},
			{Tool: "codex", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 12_400, TokensEstimated: true}, Models: []protocol.UsageModelRow{
				{Model: "gpt-5-codex", UsageRow: protocol.UsageRow{Sessions: 2, Tokens: 12_400, TokensEstimated: true}},
			}},
			{Tool: "antigravity", UsageRow: protocol.UsageRow{Sessions: 1, Tokens: 40_000, TokensEstimated: true}},
		},
		Quotas: usageQuotas(now),
	}
}

// usageModelAt is a board with the report loaded into an open :usage page.
func usageModelAt(t *testing.T, sz goldenSize) Model {
	t.Helper()
	m := goldenBoard(t, sz, false)
	rep := usageReport(goldenNow)
	m.Focus = FocusUsage
	m.UsagePage = &UsageState{Range: protocol.UsageRangeToday, Report: &rep, FetchedAt: goldenNow.Add(-12 * time.Second)}
	return m
}

// meterModel is the board with live quotas on it.
func meterModel(t *testing.T, sz goldenSize) Model {
	t.Helper()
	m := goldenBoard(t, sz, false)
	m.Usage = &protocol.Usage{Enabled: true, Quotas: usageQuotas(goldenNow)}
	return m
}

type fakeUsager struct {
	calls []protocol.UsageQuery
	rep   protocol.UsageReport
	err   error
}

func (f *fakeUsager) UsageQuery(_ context.Context, q protocol.UsageQuery) (protocol.UsageReport, error) {
	f.calls = append(f.calls, q)
	return f.rep, f.err
}

// deliver runs the command and feeds its message back, like the runtime.
func deliver(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func usagePageModel(t *testing.T) (Model, *fakeUsager) {
	t.Helper()
	m := goldenBoard(t, goldenSize{120, 40}, false)
	fu := &fakeUsager{rep: usageReport(goldenNow)}
	m.usager = fu
	return m, fu
}

func TestUsageCommand_OpensAndFetchesToday(t *testing.T) {
	m, fu := usagePageModel(t)
	m, cmd := executeCommand(m, "usage")
	require.Equal(t, FocusUsage, m.Focus)
	require.NotNil(t, cmd)
	require.True(t, m.UsagePage.Loading)
	require.Contains(t, ansi.Strip(m.View()), "loading…")
	m = deliver(t, m, cmd)
	require.Equal(t, []protocol.UsageQuery{{Range: "today"}}, fu.calls)
	require.False(t, m.UsagePage.Loading)
	require.NotNil(t, m.UsagePage.Report)
	out := ansi.Strip(m.View())
	for _, want := range []string{"USAGE", "AGENT / MODEL", "claude", "  opus", "TOTAL", "5h ▰▰▱▱▱  42%", "~88k", "$18.40"} {
		require.Contains(t, out, want)
	}
	m, _ = executeCommand(m, "usage")
	require.Equal(t, FocusBoard, m.Focus, ":usage again closes")
	require.Nil(t, m.UsagePage)
}

func TestUsagePage_RangeKeys(t *testing.T) {
	m, fu := usagePageModel(t)
	m, cmd := executeCommand(m, "usage")
	m = deliver(t, m, cmd)
	step := func(k string) {
		var cmd tea.Cmd
		m, cmd = updateKey(m, key(k))
		m = deliver(t, m, cmd)
	}
	step("right")
	require.Equal(t, "7d", m.UsagePage.Range)
	step("right")
	require.Equal(t, "30d", m.UsagePage.Range)
	step("right")
	require.Equal(t, "30d", m.UsagePage.Range, "stops at the end, no extra request")
	step("left")
	require.Equal(t, "7d", m.UsagePage.Range)
	step("3")
	require.Equal(t, "30d", m.UsagePage.Range)
	step("1")
	require.Equal(t, "today", m.UsagePage.Range)
	step("2")
	require.Equal(t, "7d", m.UsagePage.Range)
	var ranges []string
	for _, c := range fu.calls {
		ranges = append(ranges, c.Range)
	}
	require.Equal(t, []string{"today", "7d", "30d", "7d", "30d", "today", "7d"}, ranges)
}

func TestUsagePage_RefreshAndStaleReplies(t *testing.T) {
	m, fu := usagePageModel(t)
	m, cmd := executeCommand(m, "usage")
	m = deliver(t, m, cmd)
	m, cmd = updateKey(m, key("r"))
	require.NotNil(t, cmd)
	require.True(t, m.UsagePage.Loading)
	m = deliver(t, m, cmd)
	require.Len(t, fu.calls, 2, "r asks the daemon again")

	// A reply for a request the page moved past is dropped.
	m, cmdOld := updateKey(m, key("2"))
	oldMsg := cmdOld()
	m, cmdNew := updateKey(m, key("3"))
	m = deliver(t, m, cmdNew)
	require.Equal(t, "30d", m.UsagePage.Range)
	next, _ := m.Update(oldMsg)
	m = next.(Model)
	require.Equal(t, "30d", m.UsagePage.Range)
	require.NotNil(t, m.UsagePage.Report)

	// A reply after the page closed is dropped too.
	m, cmd = updateKey(m, key("r"))
	late := cmd()
	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)
	next, _ = m.Update(late)
	require.Nil(t, next.(Model).UsagePage)
}

func TestUsagePage_Filter(t *testing.T) {
	m := usageModelAt(t, goldenSize{120, 40})
	m, _ = updateKey(m, key("/"))
	require.True(t, m.UsagePage.Filtering)
	m = typeText(m, "sonn")
	out := ansi.Strip(m.View())
	require.Contains(t, out, "sonnet")
	require.Contains(t, out, "claude", "the tool of a matching model stays")
	require.NotContains(t, out, "opus")
	require.NotContains(t, out, "grok")
	require.Contains(t, out, "TOTAL (filtered)")
	require.Contains(t, out, "790k", "the total follows the filter")
	m, _ = updateKey(m, key("enter"))
	require.False(t, m.UsagePage.Filtering)
	require.Equal(t, "sonn", m.UsagePage.Filter)
	m, _ = updateKey(m, key("esc"))
	require.Empty(t, m.UsagePage.Filter, "esc clears the filter first")
	require.Equal(t, FocusUsage, m.Focus)
	m, _ = updateKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)

	m = usageModelAt(t, goldenSize{120, 40})
	m, _ = updateKey(m, key("/"))
	m = typeText(m, "zzz")
	require.Contains(t, ansi.Strip(m.View()), "nothing matches zzz")
}

func TestUsagePage_Lines(t *testing.T) {
	rep := usageReport(goldenNow)
	lines := usageLines(rep, "")
	var kinds []string
	for _, l := range lines {
		kinds = append(kinds, l.name)
	}
	require.Equal(t, []string{"claude", "", "opus", "sonnet", "grok", "grok-build", "codex", "gpt-5-codex", "antigravity"}, kinds,
		"the second claude window is its own line, models follow their tool")
	require.Equal(t, usageExtraQuota, lines[1].kind)

	// A tool with a quota and no usage in the range still shows, as dashes.
	rep.Quotas = append(rep.Quotas, protocol.UsageQuota{Tool: "cursor", Window: "month", UsedPct: fptr(5), At: goldenNow})
	last := usageLines(rep, "")[len(usageLines(rep, ""))-1]
	require.Equal(t, "cursor", last.name)
	require.True(t, last.empty)

	// A filter on the tool name keeps all its models.
	got := usageLines(rep, "CLAUDE")
	require.Len(t, got, 4)
}

func TestUsagePage_EmptyAndErrors(t *testing.T) {
	m := usageModelAt(t, goldenSize{120, 40})
	m.UsagePage.Report = &protocol.UsageReport{Range: "today"}
	require.Contains(t, ansi.Strip(m.View()), "No usage recorded in this range.")

	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"old daemon": {&client.Error{Intent: "UsageQuery", Code: protocol.ErrCodeBadIntent, Message: "intent not implemented"}, "update daemon"},
		"off":        {&client.Error{Intent: "UsageQuery", Code: protocol.ErrCodeBadIntent, Message: "usage is off"}, "off: enable in Settings → Features (rex config set usage true)"},
		"other":      {errors.New("boom"), "boom"},
	} {
		t.Run(name, func(t *testing.T) {
			m, fu := usagePageModel(t)
			fu.err = tc.err
			m, cmd := executeCommand(m, "usage")
			m = deliver(t, m, cmd)
			require.Contains(t, m.UsagePage.Err, tc.want)
			require.Contains(t, ansi.Strip(m.View()), tc.want[:min(len(tc.want), 40)])
		})
	}

	m = goldenBoard(t, goldenSize{120, 40}, false)
	m, cmd := executeCommand(m, "usage")
	require.Nil(t, cmd)
	require.Equal(t, "not connected to the daemon", m.UsagePage.Err)
}

func TestUsagePage_StaleQuotaIsDimmedWithTime(t *testing.T) {
	m := usageModelAt(t, goldenSize{120, 40})
	require.Contains(t, ansi.Strip(m.View()), "as of 11:20", "the 40 minute old reading shows its time, not a reset")
	require.True(t, usageStale(goldenNow, protocol.UsageQuota{At: goldenNow.Add(-16 * time.Minute)}))
	require.False(t, usageStale(goldenNow, protocol.UsageQuota{At: goldenNow.Add(-14 * time.Minute)}))
	require.True(t, usageStale(goldenNow, protocol.UsageQuota{At: goldenNow, Stale: true}), "the daemon's own call counts")
}

func TestQuotaStyle_Thresholds(t *testing.T) {
	cases := []struct {
		pct   float64
		stale bool
		want  string
	}{
		{0, false, "success"}, {59.9, false, "success"},
		{60, false, "warn"}, {84.9, false, "warn"},
		{85, false, "danger"}, {100, false, "danger"},
		{95, true, "dim"},
	}
	for _, c := range cases {
		fg := quotaStyle(c.pct, c.stale).GetForeground()
		var want any
		switch c.want {
		case "success":
			want = colorSuccess
		case "warn":
			want = colorWarn
		case "danger":
			want = colorDanger
		default:
			want = colorFgDim
		}
		require.Equal(t, want, fg, "%.1f%% stale=%v", c.pct, c.stale)
	}
}

func TestUsageCells(t *testing.T) {
	require.Equal(t, "42", usageTokens(42, false))
	require.Equal(t, "1.2k", usageTokens(1234, false))
	require.Equal(t, "410k", usageTokens(410_000, false))
	require.Equal(t, "1.3M", usageTokens(1_340_000, false))
	require.Equal(t, "~88k", usageTokens(88_000, true))
	require.Equal(t, "—", usageCost(nil))
	require.Equal(t, "$18.40", usageCost(fptr(18.4)))
	require.Equal(t, "▰▰▱▱▱", usageBar(42, 5))
	require.Equal(t, "▱▱▱▱▱", usageBar(0, 5))
	require.Equal(t, "▰▰▰▰▰", usageBar(100, 5))
	require.Equal(t, "2h10m", usageResetsIn(goldenNow, goldenNow.Add(2*time.Hour+10*time.Minute)))
	require.Equal(t, "35m", usageResetsIn(goldenNow, goldenNow.Add(35*time.Minute)))
	require.Equal(t, "4d", usageResetsIn(goldenNow, goldenNow.Add(4*24*time.Hour+time.Hour)))
	require.Equal(t, "now", usageResetsIn(goldenNow, goldenNow.Add(-time.Hour)))
}

// A daemon-supplied tool or model name cannot break the table or reach the
// terminal.
func TestUsagePage_UntrustedNames(t *testing.T) {
	m := usageModelAt(t, goldenSize{120, 40})
	m.UsagePage.Report.Tools[0].Tool = "cla\x1b[31mude\nx"
	for _, l := range strings.Split(m.View(), "\n") {
		require.NotContains(t, l, "\x1b[31m")
	}
	require.Len(t, strings.Split(m.View(), "\n"), 40)
}

func TestUsagePage_ScrollsOnAShortScreen(t *testing.T) {
	m := usageModelAt(t, goldenSize{80, 14})
	require.Len(t, strings.Split(m.View(), "\n"), 14)
	require.Contains(t, ansi.Strip(m.View()), "↓ more")
	m, _ = updateKey(m, key("G"))
	out := ansi.Strip(m.View())
	require.Contains(t, out, "antigravity")
	require.Contains(t, out, "↑ more")
	require.Contains(t, out, "TOTAL", "the total stays pinned")
}

// usage off is inert: the command refuses, no request is sent, the meter is
// hidden and a click on its place does nothing.
func TestUsageOff_IsInert(t *testing.T) {
	m := meterModel(t, goldenSize{120, 40})
	fu := &fakeUsager{}
	m.usager = fu
	m.Store = flagOff(t, "usage")
	require.Contains(t, ansi.Strip(renderHeader(m, 118)), "REX")
	require.NotContains(t, ansi.Strip(renderHeader(m, 118)), "▰", "meter hidden")
	m, cmd := executeCommand(m, "usage")
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, "off: enable in Settings → Features (rex config set usage true)", m.Err)
	require.Empty(t, fu.calls)
	require.False(t, headerMeterHit(m, 100, 1))
}

// A new range does not show the old range's table while it loads.
func TestUsagePage_RangeChangeClearsTheOldTable(t *testing.T) {
	m, _ := usagePageModel(t)
	m, cmd := executeCommand(m, "usage")
	m = deliver(t, m, cmd)
	require.Contains(t, ansi.Strip(m.View()), "claude")
	m, cmd = updateKey(m, key("2"))
	out := ansi.Strip(m.View())
	require.Contains(t, out, "loading…")
	require.NotContains(t, out, "AGENT / MODEL")
	m = deliver(t, m, cmd)
	require.Contains(t, ansi.Strip(m.View()), "claude")
}
