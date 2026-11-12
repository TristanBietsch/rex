package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func simpleBoard(sessions ...protocol.SessionSummary) Model {
	m := boardModel(sessions...)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.Clock = func() time.Time { return now }
	return m
}

func boardText(m Model) string { return ansi.Strip(renderBoard(m, 140, 0)) }

func lineWith(t *testing.T, text, needle string) string {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	t.Fatalf("no line contains %q in:\n%s", needle, text)
	return ""
}

// The simple row is state, name, text and age; v adds id, model · effort,
// tokens and the AI description.
func TestSimpleRowAndDetailedToggle(t *testing.T) {
	s := protocol.SessionSummary{ID: "a", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Effort: "max",
		Tokens: 123456, Slug: "dark-mode", Title: "add a dark mode toggle", Description: "Claude is editing styles.go",
		State: protocol.StateWorking, LastEventAt: time.Date(2026, 1, 1, 11, 56, 0, 0, time.UTC)}
	m := simpleBoard(s)

	row := lineWith(t, boardText(m), "dark-mode")
	require.Contains(t, row, "add a dark mode toggle", "the task, not the AI line")
	require.Contains(t, row, "4m")
	for _, hidden := range []string{"7d4f", "opus", "123", "editing styles.go"} {
		require.NotContains(t, row, hidden)
	}

	m, _ = updateKey(m, key("v"))
	require.True(t, m.Detailed)
	row = lineWith(t, boardText(m), "dark-mode")
	for _, shown := range []string{"7d4f", "opus · max", "123K tk", "Claude is editing styles.go"} {
		require.Contains(t, row, shown)
	}
	require.Contains(t, ansi.Strip(renderHeader(m, 140)), "detailed (v)")

	m, _ = updateKey(m, key("v"))
	require.False(t, m.Detailed)
}

func TestRowText(t *testing.T) {
	cases := []struct {
		name string
		s    protocol.SessionSummary
		want string
	}{
		{"waiting with a question shows it",
			protocol.SessionSummary{State: protocol.StateNeedsInput, Title: "task", LastLine: "Which feature should lead: search or export?"},
			"Which feature should lead: search or export?"},
		{"waiting on agent chrome shows the task",
			protocol.SessionSummary{State: protocol.StateNeedsInput, Title: "task", LastLine: "✔ Update installed · Restart to update"},
			"task"},
		{"a mid-line ? is not a question",
			protocol.SessionSummary{State: protocol.StateNeedsInput, Title: "task", LastLine: "new task? /clear to save 130.7k tokens"},
			"task"},
		{"working shows the task, not the AI line",
			protocol.SessionSummary{State: protocol.StateWorking, Title: "task", Description: "Claude is running tests"},
			"task"},
		{"marked done ignores a question",
			protocol.SessionSummary{State: protocol.StateNeedsInput, Title: "task", LastLine: "Ship it?", RetireAt: time.Now()},
			"task"},
		{"no task falls back to the AI line",
			protocol.SessionSummary{State: protocol.StateWorking, Description: "Claude is running tests"},
			"Claude is running tests"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rowText(tc.s))
		})
	}
}

func TestFilterChipsOnlyWhenUseful(t *testing.T) {
	one := simpleBoard(
		protocol.SessionSummary{ID: "a", ToolID: "claude", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "b", ToolID: "claude", State: protocol.StateDone},
	)
	require.False(t, showFilterChips(one))
	require.Equal(t, 1, headerHeight(one))
	require.NotContains(t, ansi.Strip(renderHeader(one, 120)), "codex")
	require.Equal(t, "", headerHitAt(one, 4, 2), "no chip row to click")

	two := one
	two.Sessions = append(append([]protocol.SessionSummary(nil), one.Sessions...),
		protocol.SessionSummary{ID: "c", ToolID: "codex", State: protocol.StateWorking})
	require.True(t, showFilterChips(two))
	require.Equal(t, 2, headerHeight(two))

	filtered := one
	filtered.Filter = "codex"
	require.True(t, showFilterChips(filtered), "an active filter stays visible so it can be cleared")

	// The layout follows the header height: the board starts at boardTop.
	for _, m := range []Model{one, two} {
		screen := strings.Split(renderFullScreen(m, 120, 30), "\n")
		require.Len(t, screen, 30)
		require.Contains(t, ansi.Strip(screen[boardTop(m)]), "READY")
	}
}

func doneSessions(n int) []protocol.SessionSummary {
	out := []protocol.SessionSummary{{ID: "live", Slug: "live", State: protocol.StateNeedsInput}}
	for i := 0; i < n; i++ {
		out = append(out, protocol.SessionSummary{ID: fmt.Sprintf("d%d", i), Slug: fmt.Sprintf("done-%d", i), State: protocol.StateDone})
	}
	return out
}

func TestDoneCollapsesPastFive(t *testing.T) {
	short := simpleBoard(doneSessions(doneCollapseAt)...)
	require.False(t, doneCollapsed(short))
	require.Contains(t, boardText(short), "done-4")

	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	require.True(t, doneCollapsed(m))
	text := boardText(m)
	require.NotContains(t, text, "done-0")
	title := lineWith(t, text, "DONE")
	require.Contains(t, title, "6 hidden")
	require.NotContains(t, title, "4 show", "the fold keys live in the help line")
	require.NotContains(t, title, ":prune")
	help := ansi.Strip(renderHelpLine(m, 200))
	require.Contains(t, help, "4 show")
	require.Contains(t, help, ":prune clear")
	require.Equal(t, []string{"live"}, sessionIDs(orderedSessions(m)), "hidden rows are not navigable")

	m, _ = updateKey(m, key("4"))
	require.True(t, m.DoneExpanded)
	require.Equal(t, "d0", m.SelectedID, "4 expands and selects the first Done row")
	require.Contains(t, boardText(m), "done-5")
	require.Contains(t, ansi.Strip(renderHelpLine(m, 200)), "4 hide")

	m, _ = updateKey(m, key("4"))
	require.False(t, m.DoneExpanded)
	require.Equal(t, "live", m.SelectedID, "folding moves the selection off the hidden rows")
}

// A row marked done drops into a folded Done column; the selection follows
// to a row the board still shows.
func TestSelectionLeavesRowThatFoldsAway(t *testing.T) {
	sessions := doneSessions(doneCollapseAt)
	sessions = append(sessions, protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking})
	m := simpleBoard(sessions...)
	m.SelectedID = "w"
	raw, err := json.Marshal(protocol.SessionUpdated{SessionID: "w", Patch: map[string]any{"retire_at": "2026-01-09T03:04:05Z"}})
	require.NoError(t, err)
	next, _ := m.Update(DaemonEventMsg{Env: protocol.Envelope{Type: protocol.EventSessionUpdated, Data: raw}})
	m = next.(Model)
	require.True(t, doneCollapsed(m), "six Done rows now")
	require.Equal(t, "live", m.SelectedID)
}

func TestTabCyclesSessionsThatNeedYou(t *testing.T) {
	m := simpleBoard(
		protocol.SessionSummary{ID: "n1", Slug: "n1", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "n2", Slug: "n2", State: protocol.StateNeedsInput},
	)
	m.SelectedID = "w"
	m, _ = updateKey(m, key("tab"))
	require.Equal(t, "n1", m.SelectedID)
	m, _ = updateKey(m, key("tab"))
	require.Equal(t, "n2", m.SelectedID)
	m, _ = updateKey(m, key("tab"))
	require.Equal(t, "n1", m.SelectedID, "wraps")

	none := simpleBoard(protocol.SessionSummary{ID: "w", Slug: "w", State: protocol.StateWorking})
	none, _ = updateKey(none, key("tab"))
	require.Equal(t, "w", none.SelectedID)
	require.Equal(t, "nothing ready", none.Notice)
}

func TestHelpBarHasFiveKeys(t *testing.T) {
	m := simpleBoard(protocol.SessionSummary{ID: "a", State: protocol.StateWorking})
	line := strings.TrimSpace(ansi.Strip(renderHelpLine(m, 200)))
	require.Equal(t, "enter open · i new · cc done · tab next · ? more", line)
}

// 4 on a short Done column just jumps to Done.
func TestFourOnShortDoneJumps(t *testing.T) {
	m := simpleBoard(doneSessions(2)...)
	m.SelectedID = "live"
	m, _ = updateKey(m, key("4"))
	require.Equal(t, "d0", m.SelectedID)
	require.False(t, m.DoneExpanded)
}

// An expanded Done column that shrinks back to doneCollapseAt folds again
// the next time it grows.
func TestDoneFoldsAgainAfterShrinking(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	m, _ = updateKey(m, key("4"))
	require.True(t, m.DoneExpanded)

	m.Sessions = m.Sessions[:len(m.Sessions)-2] // e.g. two pruned
	next, _ := m.Update(SpinnerTickMsg{Gen: -1})
	m = next.(Model)
	require.False(t, m.DoneExpanded, "short again: back to folding")

	m.Sessions = simpleBoard(doneSessions(doneCollapseAt + 1)...).Sessions
	require.True(t, doneCollapsed(m))
}

// The folded title stays one board-width line on narrow terminals.
func TestFoldedTitleFitsWidth(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 2)...)
	for _, w := range []int{20, 30, 44, 80} {
		title := groupTitle(m, groupDone, w)
		require.NotContains(t, title, "\n")
		require.Equal(t, w, ansi.StringWidth(title), "width %d", w)
	}
	for _, line := range strings.Split(renderBoard(m, 30, 0), "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), 30)
	}
}

// With the chip row hidden, a click on header row 1 (the summary banner,
// whose text contains "all") must not reset the filter.
func TestHeaderClickWithoutChipRow(t *testing.T) {
	m := simpleBoard(protocol.SessionSummary{ID: "a", ToolID: "claude", State: protocol.StateWorking})
	m.BackendUnavailable = true
	m.BackendUnavailableReason = "ollama unreachable"
	require.False(t, showFilterChips(m))
	banner := ansi.Strip(strings.Split(renderHeader(m, 140), "\n")[1])
	x := strings.Index(banner, "all")
	require.GreaterOrEqual(t, x, 0, "the banner contains \"all\" (in install)")
	require.Equal(t, "", headerHitAt(m, x, 2))
}

// j/k on a board with a folded Done column never select a hidden row.
func TestNavigationSkipsFoldedRows(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 3)...)
	m.SelectedID = "live"
	for range 5 {
		m, _ = updateKey(m, key("j"))
		require.Equal(t, "live", m.SelectedID)
	}
	m, _ = updateKey(m, key("G"))
	require.Equal(t, "live", m.SelectedID)
}

func TestSubagentColumn(t *testing.T) {
	busy := protocol.SessionSummary{ID: "a", ShortID: "aaaa", Slug: "busy", Title: "split the work", ToolID: "claude",
		State: protocol.StateWorking, Subagents: 3}
	calm := protocol.SessionSummary{ID: "b", ShortID: "bbbb", Slug: "calm", Title: "one thing", ToolID: "claude",
		State: protocol.StateWorking}

	none := simpleBoard(calm)
	require.Positive(t, boardLayout(none, 140).agents, "fixed: the column is reserved while nothing runs subagents")
	require.Zero(t, boardLayout(fitModel(t, none), 140).agents, "fit: no column while nothing runs subagents")
	require.NotContains(t, boardText(none), subagentGlyph)

	m := simpleBoard(busy, calm)
	require.Contains(t, lineWith(t, boardText(m), "busy"), subagentGlyph+" 3")
	require.NotContains(t, lineWith(t, boardText(m), "calm"), subagentGlyph)
	m.Detailed = true
	require.Contains(t, lineWith(t, boardText(m), "busy"), subagentGlyph+" 3")

	big := busy
	big.Subagents = 150
	require.Contains(t, lineWith(t, boardText(simpleBoard(big)), "busy"), subagentGlyph+" 99")

	// Rows stay exactly the board width with the column, down to narrow.
	for _, detailed := range []bool{false, true} {
		m.Detailed = detailed
		for _, w := range []int{40, 60, 80, 120} {
			for _, line := range strings.Split(renderBoard(m, w, 0), "\n") {
				require.LessOrEqual(t, ansi.StringWidth(line), w)
			}
			for _, s := range m.Sessions {
				require.Equal(t, w, ansi.StringWidth(renderRow(m, s, boardLayout(m, w))), "detailed=%v width %d", detailed, w)
			}
		}
	}
}

func TestSubagentPatch(t *testing.T) {
	m := simpleBoard(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	patch := func(v any) {
		raw, err := json.Marshal(protocol.SessionUpdated{SessionID: "a", Patch: map[string]any{"subagents": v}})
		require.NoError(t, err)
		m = m.applyEvent(protocol.Envelope{Type: protocol.EventSessionUpdated, Data: raw})
	}
	patch(4)
	require.Equal(t, 4, m.Sessions[0].Subagents)
	patch(0)
	require.Zero(t, m.Sessions[0].Subagents)
	patch(-1)
	require.Zero(t, m.Sessions[0].Subagents, "a negative count is ignored")
}
