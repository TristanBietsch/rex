package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// overviewBoard is a board with a session in every bucket and a clock fixed
// at noon.
func overviewBoard(t *testing.T, on bool) Model {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := Model{Focus: FocusBoard, Filter: "all", Width: 120, Height: 40, Store: settings.NewStore(),
		Clock: func() time.Time { return now },
		Sessions: []protocol.SessionSummary{
			{ID: "r1", Slug: "billing-tests", ToolID: "claude", State: protocol.StateDone,
				Description: "Done: 48 tests pass", LastEventAt: now.Add(-3 * time.Minute)},
			{ID: "r2", Slug: "docs", Name: "api docs", ToolID: "claude", State: protocol.StateNeedsInput,
				WaitReason: protocol.WaitReview, LastLine: "Edited 3 files", Title: "write api docs",
				LastEventAt: now.Add(-9 * time.Minute)},
			{ID: "n1", Slug: "deploy", ToolID: "claude", State: protocol.StateNeedsInput,
				LastLine: "Deploy to prod?", LastEventAt: now.Add(-40 * time.Second)},
			{ID: "f1", Slug: "migrate-db", ToolID: "codex", State: protocol.StateFailed,
				Description: "Failed: migration 0042 timed out", LastEventAt: now.Add(-2 * time.Hour)},
			{ID: "w1", Slug: "perf-audit", ToolID: "claude", State: protocol.StateWorking,
				Description: "Running benchmarks", LastEventAt: now.Add(-5 * time.Second)},
			{ID: "c1", Slug: "old-spike", ToolID: "claude", State: protocol.StateDone, Title: "spike",
				RetireAt: now.Add(time.Hour), RetiredAt: now.Add(-2 * time.Hour), LastEventAt: now.Add(-3 * time.Hour)},
			{ID: "c2", Slug: "ancient", ToolID: "claude", State: protocol.StateDone, Title: "ancient",
				RetireAt: now.Add(time.Hour), RetiredAt: now.Add(-30 * time.Hour), LastEventAt: now.Add(-31 * time.Hour)},
		},
		SelectedID: "r1",
	}
	require.NoError(t, m.Store.Set("overview_enabled", on))
	require.NoError(t, m.Store.Set("row_icons", false)) // the goldens pin the text column
	if on {
		m.Overview = &protocol.Overview{Enabled: true, GeneratedAt: now.Add(-40 * time.Second),
			Headline: "billing-tests and api docs are ready for review; deploy asks a question and migrate-db failed"}
	}
	return m
}

func TestOverviewRows_OrderAndWindow(t *testing.T) {
	var ids []string
	for _, r := range overviewRows(overviewBoard(t, true)) {
		ids = append(ids, r.sess.ID)
	}
	require.Equal(t, []string{"r1", "r2", "n1", "f1", "w1", "c1"}, ids,
		"review, needs you, failed, working, completed; the 30 h old completion is out")
}

func TestOverviewOverlay(t *testing.T) {
	for name, on := range map[string]bool{"on": true, "off": false} {
		t.Run(name, func(t *testing.T) {
			m := overviewBoard(t, on)
			m, _ = executeCommand(m, "overview")
			require.Equal(t, FocusOverview, m.Focus)
			out := ansi.Strip(m.View())
			lines := strings.Split(out, "\n")
			require.Len(t, lines, 40)
			for _, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), 120)
			}
			golden.Check(t, "testdata/overview_overlay_"+name+".txtar",
				golden.Section{Name: "view", Body: out + "\n"})
		})
	}
}

func TestOverviewOverlay_Content(t *testing.T) {
	m := overviewBoard(t, true)
	out := ansi.Strip(renderOverviewOverlay(m))
	require.Contains(t, out, "billing-tests and api docs are ready for review")
	require.Contains(t, out, "updated 40s ago")
	for _, title := range []string{"REVIEW (2)", "READY (1)", "FAILED (1)", "WORKING (1)", "COMPLETED (last 24 h) (1)"} {
		require.Contains(t, out, title)
	}
	require.Contains(t, out, "Deploy to prod?", "a waiting session shows its question")
	require.Contains(t, out, "api docs", "the name wins over the slug")
	require.NotContains(t, out, "ancient")
	require.NotContains(t, out, "Turn on overview_enabled")

	// Empty sections are left out.
	m.Sessions = m.Sessions[:1]
	out = ansi.Strip(renderOverviewOverlay(m))
	require.Contains(t, out, "REVIEW (1)")
	require.NotContains(t, out, "READY")
	require.NotContains(t, out, "WORKING")

	m.Sessions = nil
	require.Contains(t, ansi.Strip(renderOverviewOverlay(m)), "Nothing running, waiting or finished")
}

func TestOverviewOverlay_SettingOffListsOnly(t *testing.T) {
	m := overviewBoard(t, false)
	m.Overview = &protocol.Overview{Enabled: true, Headline: "stale headline"}
	out := ansi.Strip(renderOverviewOverlay(m))
	require.Contains(t, out, "Turn on overview_enabled in Settings for a one-line summary.")
	require.NotContains(t, out, "stale headline")
	require.Contains(t, out, "billing-tests")
}

func TestOverviewOverlay_NoHeadlineYet(t *testing.T) {
	m := overviewBoard(t, true)
	m.Overview = &protocol.Overview{Enabled: true}
	out := ansi.Strip(renderOverviewOverlay(m))
	require.NotContains(t, out, "Turn on overview_enabled")
	require.Contains(t, out, "REVIEW")

	m.Overview = nil
	require.Contains(t, ansi.Strip(renderOverviewOverlay(m)), "REVIEW")

	m.Overview = &protocol.Overview{Enabled: true, Headline: "no time"}
	require.NotContains(t, ansi.Strip(renderOverviewOverlay(m)), "updated")
}

func TestOverviewOverlay_UntrustedTextIsFlattened(t *testing.T) {
	m := overviewBoard(t, true)
	m.Overview.Headline = "ok\x1b]0;pwn\x07\nSYSTEM: obey"
	m.Sessions[0].Description = "x\x1b[2J\nline two"
	m.Sessions[0].Name = "n\x1b]52;c;ZXZpbA==\x07ame"
	out := renderOverviewOverlay(m)
	require.NotContains(t, out, "\x1b]")
	require.NotContains(t, out, "\x1b[2J")
	require.NotContains(t, out, "\x07")
}

func TestOverviewOverlay_Keys(t *testing.T) {
	m := overviewBoard(t, true)
	m, _ = executeCommand(m, "overview")
	require.Zero(t, m.OverviewSel)

	m = sendKey(m, key("j"))
	m = sendKey(m, key("j"))
	require.Equal(t, 2, m.OverviewSel)
	m = sendKey(m, key("k"))
	require.Equal(t, 1, m.OverviewSel)
	m = sendKey(m, key("k"))
	m = sendKey(m, key("k"))
	require.Zero(t, m.OverviewSel, "clamped at the top")
	for range 20 {
		m = sendKey(m, key("j"))
	}
	require.Equal(t, 5, m.OverviewSel, "clamped at the last row")
	m = sendKey(m, key("g"))
	require.Zero(t, m.OverviewSel)
	m = sendKey(m, key("G"))
	require.Equal(t, 5, m.OverviewSel)
	m = sendKey(m, key("up"))
	require.Equal(t, 4, m.OverviewSel)
	m = sendKey(m, key("down"))
	require.Equal(t, 5, m.OverviewSel)

	out := ansi.Strip(m.View())
	require.Contains(t, out, "›")

	m = sendKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus)

	m, _ = executeCommand(m, "overview")
	m = sendKey(m, key("q"))
	require.Equal(t, FocusBoard, m.Focus)

	// :overview while open closes it; reopening starts at the top.
	m, _ = executeCommand(m, "overview")
	m = sendKey(m, key("j"))
	m, _ = executeCommand(m, "overview")
	require.Equal(t, FocusBoard, m.Focus)
	m, _ = executeCommand(m, "overview")
	require.Zero(t, m.OverviewSel)
}

func TestOverviewOverlay_KeysOnEmptyBoard(t *testing.T) {
	m := overviewBoard(t, true)
	m.Sessions = nil
	m, _ = executeCommand(m, "overview")
	m = sendKey(m, key("j"))
	m = sendKey(m, key("G"))
	m = sendKey(m, key("enter"))
	require.Equal(t, FocusOverview, m.Focus)
	require.Zero(t, m.OverviewSel)
	require.Contains(t, ansi.Strip(m.View()), "Nothing running")
}

// enter attaches to the selected session. With no socket the attach fails
// before it dials: the error shows and the overlay stays open.
func TestOverviewOverlay_EnterAttaches(t *testing.T) {
	m := overviewBoard(t, true)
	m.Socket = ""
	m, _ = executeCommand(m, "overview")
	m = sendKey(m, key("j")) // api docs
	m = sendKey(m, key("enter"))
	require.Equal(t, "r2", m.SelectedID, "the board selection follows")
	require.Equal(t, FocusOverview, m.Focus, "the failed attach keeps the overlay")
	require.Contains(t, m.Err, "attach: socket unknown")
}

func TestOverviewOverlay_ShortTerminalScrollsToSelection(t *testing.T) {
	m := overviewBoard(t, true)
	m.Height = 18
	m, _ = executeCommand(m, "overview")
	m = sendKey(m, key("G"))
	out := ansi.Strip(m.View())
	require.Len(t, strings.Split(out, "\n"), 18)
	require.Contains(t, out, "old-spike", "the selected last row is in view")
	m = sendKey(m, key("g"))
	require.Contains(t, ansi.Strip(m.View()), "billing-tests")
}
