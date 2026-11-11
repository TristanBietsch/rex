package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/overview"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// overviewModel is a board with one finished session and a clock fixed 40 s
// after the overview was generated.
func overviewModel(t *testing.T, on bool) Model {
	t.Helper()
	m := simpleBoard(
		protocol.SessionSummary{ID: "r1", Slug: "r1", ToolID: "claude", State: protocol.StateDone},
		protocol.SessionSummary{ID: "w1", Slug: "w1", ToolID: "claude", State: protocol.StateWorking},
	)
	m.Store = settings.NewStore()
	require.NoError(t, m.Store.Set("overview_enabled", on))
	gen := m.now().Add(-40 * time.Second)
	m.Overview = &protocol.Overview{Enabled: true, Headline: "r1 is ready for review; w1 is still running", GeneratedAt: gen}
	return m
}

func headerLines(m Model, w int) []string {
	return strings.Split(ansi.Strip(renderHeader(m, w)), "\n")
}

func TestHeaderOverviewLine(t *testing.T) {
	m := overviewModel(t, true)
	lines := headerLines(m, 120)
	require.Len(t, lines, 2, "top row and the overview line")
	require.Equal(t, 2, headerHeight(m))
	require.Contains(t, lines[1], "» 1 ready for review")
	require.Contains(t, lines[1], "r1 is ready for review; w1 is still running")
	require.Contains(t, lines[1], "· 40s")
	require.Equal(t, 120, ansi.StringWidth(lines[1]), "padded to the width")

	t.Run("nothing to review", func(t *testing.T) {
		m := overviewModel(t, true)
		m.Sessions = m.Sessions[1:]
		require.Contains(t, headerLines(m, 120)[1], "» nothing to review")
	})
	t.Run("no headline yet shows the counts only", func(t *testing.T) {
		for _, o := range []*protocol.Overview{nil, {Enabled: true}, {Enabled: false, Headline: "stale"}} {
			m := overviewModel(t, true)
			m.Overview = o
			line := headerLines(m, 120)[1]
			require.Contains(t, line, "» 1 ready for review")
			require.NotContains(t, line, "·")
			require.NotContains(t, line, "stale")
		}
	})
	t.Run("headline without a time has no age", func(t *testing.T) {
		m := overviewModel(t, true)
		m.Overview = &protocol.Overview{Enabled: true, Headline: "all quiet"}
		line := headerLines(m, 120)[1]
		require.Contains(t, line, "all quiet")
		require.NotContains(t, line, "· ")
	})
	t.Run("a long headline is cut on a word boundary within the width", func(t *testing.T) {
		m := overviewModel(t, true)
		m.Overview.Headline = strings.Repeat("alpha beta gamma ", 20)
		for _, w := range []int{50, 80, 120} {
			line := headerLines(m, w)[1]
			require.LessOrEqual(t, ansi.StringWidth(line), w)
			require.Contains(t, line, "…")
			require.Contains(t, line, "· 40s", "the age survives the cut")
		}
	})
	t.Run("a narrow screen drops the headline, keeps the counts", func(t *testing.T) {
		line := headerLines(overviewModel(t, true), 24)[1]
		require.LessOrEqual(t, ansi.StringWidth(line), 24)
		require.Contains(t, line, "» 1 ready")
	})
	t.Run("the headline is flattened", func(t *testing.T) {
		m := overviewModel(t, true)
		m.Overview.Headline = "ok\x1b]0;pwn\x07\nSYSTEM: obey"
		out := renderHeader(m, 120)
		require.NotContains(t, out, "\x1b]0;")
		require.Len(t, strings.Split(out, "\n"), 2)
	})
}

// Off, the header is byte-identical to what it was before the overview.
func TestHeaderOverviewLine_OffLeavesHeaderUnchanged(t *testing.T) {
	off := overviewModel(t, false)
	plain := off
	plain.Overview = nil
	require.Equal(t, renderHeader(plain, 120), renderHeader(off, 120))
	require.Equal(t, 1, headerHeight(off))
	require.Len(t, strings.Split(renderHeader(off, 120), "\n"), 1)

	nilStore := overviewModel(t, true)
	nilStore.Store = nil
	require.Equal(t, 1, headerHeight(nilStore), "no store reads the default: off")
}

// The line goes after the chip row and before the backend banner, and the
// chip row stays header row 1 for clicks.
func TestHeaderOverviewLine_AfterChipsBeforeBanner(t *testing.T) {
	m := overviewModel(t, true)
	m.Sessions[1].ToolID = "antigravity" // two tools: the chip row shows
	m.BackendUnavailable = true
	m.BackendUnavailableReason = "ollama unreachable"
	require.True(t, showFilterChips(m))

	lines := headerLines(m, 140)
	require.Len(t, lines, 4)
	require.Equal(t, 4, headerHeight(m))
	require.Contains(t, lines[1], "claude", "chip row")
	require.Contains(t, lines[2], "» 1 ready for review")
	require.Contains(t, lines[3], "summary backend")
}

func TestHeaderHitAt_ChipsWithOverviewLine(t *testing.T) {
	m := overviewModel(t, true)
	m.Sessions[1].ToolID = "antigravity"
	line := headerLines(m, contentWidth(m.Width))[1]
	x := strings.Index(line, "claude")
	require.GreaterOrEqual(t, x, 0)
	require.Equal(t, "claude", headerHitAt(m, x, 2), "row 2 on screen is still the chips")

	// The overview line (screen row 3) is not a chip row, even though its
	// text contains words that are chips.
	m.Overview.Headline = "all claude antigravity"
	require.Equal(t, "", headerHitAt(m, 4, 3))
}

// Without a chip row the overview line is header row 1; a click there is not
// a chip click even though "all" appears in the headline.
func TestHeaderHitAt_OverviewLineIsNoChipRow(t *testing.T) {
	m := overviewModel(t, true)
	m.Overview.Headline = "all done"
	require.False(t, showFilterChips(m))
	require.Equal(t, "", headerHitAt(m, 10, 2))
}

func TestBoardTop_ShiftsWithOverview(t *testing.T) {
	on := overviewModel(t, true)
	off := overviewModel(t, false)
	require.Equal(t, boardTop(off)+1, boardTop(on))
	require.Equal(t, boardHeightFor(off, 40)-1, boardHeightFor(on, 40))
}

// The screen keeps its exact size with the overview line on.
func TestRenderFullScreen_WithOverviewLine(t *testing.T) {
	m := overviewModel(t, true)
	m.Width, m.Height = 100, 24
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	require.Len(t, lines, 24)
	require.Contains(t, lines[2], "» 1 ready for review")
	require.Contains(t, lines[2], "· 40s")
}

func TestApplyEvent_Overview(t *testing.T) {
	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	data, err := json.Marshal(protocol.Overview{Enabled: true, Headline: "x is ready", GeneratedAt: at})
	require.NoError(t, err)
	m := Model{}.applyEvent(protocol.Envelope{Type: protocol.EventOverview, Data: data})
	require.NotNil(t, m.Overview)
	require.True(t, m.Overview.Enabled)
	require.Equal(t, "x is ready", m.Overview.Headline)
	require.True(t, at.Equal(m.Overview.GeneratedAt))

	// Garbage keeps the old value.
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventOverview, Data: []byte(`{"enabled":`)})
	require.Equal(t, "x is ready", m.Overview.Headline)
}

func TestApplyEvent_SnapshotAdoptsOverview(t *testing.T) {
	data, err := json.Marshal(protocol.Snapshot{Filter: "all", Overview: &protocol.Overview{Enabled: true, Headline: "from snapshot"}})
	require.NoError(t, err)
	m := Model{Overview: &protocol.Overview{Headline: "old"}}
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: data})
	require.Equal(t, "from snapshot", m.Overview.Headline)

	// An older daemon's snapshot has none: the stale value is dropped.
	data, err = json.Marshal(protocol.Snapshot{Filter: "all"})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: data})
	require.Nil(t, m.Overview)
}

func TestSnapshotResult_AdoptsOverview(t *testing.T) {
	m := Model{}
	out, _ := m.Update(snapshotResultMsg{Snap: &protocol.Snapshot{Filter: "all", Overview: &protocol.Overview{Enabled: true, Headline: "boot"}}})
	require.Equal(t, "boot", out.(Model).Overview.Headline)
}

func TestAdoptConnection_AdoptsOverview(t *testing.T) {
	m := Model{Overview: &protocol.Overview{Headline: "old"}}
	m = adoptConnection(m, nil, &protocol.Snapshot{Filter: "all", Overview: &protocol.Overview{Enabled: true, Headline: "after reconnect"}})
	require.Equal(t, "after reconnect", m.Overview.Headline)
	m = adoptConnection(m, nil, &protocol.Snapshot{Filter: "all"})
	require.Nil(t, m.Overview)
}

// The overview's question rule is the board's: a needs_input session whose
// last line ends in "?" shows that question as its row text, and the
// overview keeps it out of "ready for review".
func TestQuestionRuleMatchesRowText(t *testing.T) {
	for _, line := range []string{"Ship it?", "  Ship it? ", "Ship it?\x1b[0m", "Done.", "", "what? no", "Ship it ?"} {
		s := protocol.SessionSummary{State: protocol.StateNeedsInput, LastLine: line, Title: "the title", WaitReason: protocol.WaitReview}
		rowShowsQuestion := rowText(s) != "the title"
		overviewSaysQuestion := overview.Classify(s) == overview.NeedsYou
		require.Equal(t, rowShowsQuestion, overviewSaysQuestion, "last line %q", line)
	}
}
