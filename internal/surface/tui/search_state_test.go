package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// doneHeavyModel has seven Done rows (past the fold at five), one Working
// row and one Needs-you row.
func doneHeavyModel() Model {
	sessions := []protocol.SessionSummary{
		{ID: "n1", Slug: "ask-me", ToolID: "claude", State: protocol.StateNeedsInput},
		{ID: "w1", Slug: "busy", ToolID: "claude", State: protocol.StateWorking},
	}
	for i := 0; i < 7; i++ {
		id := fmt.Sprintf("done-%d", i)
		sessions = append(sessions, protocol.SessionSummary{ID: id, Slug: id, ToolID: "claude", State: protocol.StateDone})
	}
	return boardModel(sessions...)
}

func TestSearchUnfoldsDone(t *testing.T) {
	m := doneHeavyModel()
	require.True(t, doneCollapsed(m), "folded without a search")
	require.Len(t, rowIDs(m), 2)

	m = typeKeys(m, "/", "d", "o", "n", "e", "-", "6")
	require.False(t, doneCollapsed(m))
	require.Equal(t, []string{"done-6"}, rowIDs(m), "the hit is visible and navigable")
	require.Equal(t, "done-6", m.SelectedID)

	m = typeKeys(m, "enter", "esc")
	require.True(t, doneCollapsed(m), "folds again once the search is cleared")
}

func TestSearchKeepsDoneExpandedState(t *testing.T) {
	m := doneHeavyModel()
	m.DoneExpanded = true
	m = typeKeys(m, "/", "d", "o", "n", "e", "-", "6", "enter")
	require.True(t, m.DoneExpanded, "a narrowed Done list must not reset the user's unfold")
	m = typeKeys(m, "esc")
	require.True(t, m.DoneExpanded)
	require.False(t, doneCollapsed(m))
}

func TestSearchSectionCounts(t *testing.T) {
	m := searchModel()
	title := func(gi int) string { return ansi.Strip(groupTitleText(m, gi)) }
	require.Contains(t, title(groupWorking), "WORKING  2")
	require.NotContains(t, title(groupWorking), "/")

	m = typeKeys(m, "/", "a", "u", "t", "h", "enter")
	require.Contains(t, title(groupNeeds), "NEEDS YOU  1/1")
	require.Contains(t, title(groupWorking), "WORKING  1/2")
	require.Contains(t, title(groupDone), "DONE  1/1")
}

func TestToolFilterSectionCounts(t *testing.T) {
	m, _ := executeCommand(filterModel(), "filter claude")
	require.Contains(t, ansi.Strip(groupTitleText(m, groupWorking)), "WORKING  0/2")
	require.Contains(t, ansi.Strip(groupTitleText(m, groupNeeds)), "NEEDS YOU  1/1")
}

func TestSearchNoMatchesText(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", "b", "i", "l", "enter")
	plain := ansi.Strip(renderBoard(m, 100, 20))
	require.Contains(t, plain, "no matches", "empty sections say so while searching")
	require.NotContains(t, plain, "nothing waiting on you")

	m = typeKeys(m, "esc")
	plain = ansi.Strip(renderBoard(m, 100, 20))
	require.NotContains(t, plain, "no matches")
}

func TestSearchFilterBarSegment(t *testing.T) {
	m := searchModel()
	require.Empty(t, renderFilterStatus(m))

	m = typeKeys(m, "/", "a", "u", "t", "h", "enter")
	require.True(t, showFilterChips(m), "a search earns the bar even with one tool")
	require.Equal(t, 2, headerHeight(m))
	require.Equal(t, "/auth · 3 matches · esc clear", ansi.Strip(renderFilterStatus(m)))
	require.Contains(t, ansi.Strip(renderHeader(m, 120)), "/auth · 3 matches · esc clear")

	m = typeKeys(m, "/")
	require.NotContains(t, ansi.Strip(renderFilterStatus(m)), "esc clear", "the editor has its own hints")

	m = typeKeys(m, "esc", "esc")
	require.Empty(t, renderFilterStatus(m))
}

func TestSearchFilterBarTruncatesQuery(t *testing.T) {
	m := searchModel()
	m.Search = strings.Repeat("q", 100)
	status := ansi.Strip(renderFilterStatus(m))
	require.Contains(t, status, "…")
	require.Contains(t, status, "no matches")
	require.Less(t, ansi.StringWidth(status), 80)
}

func TestSearchFilterBarSanitizesQuery(t *testing.T) {
	m := searchModel()
	m.Search = "a\x1b[31m\nb"
	require.NotContains(t, renderFilterStatus(m), "\x1b[31m")
	require.NotContains(t, renderFilterStatus(m), "\n")
}

func TestSearchWithHiddenSectionHint(t *testing.T) {
	m := searchModel()
	m = setSectionHidden(m, "done", true)
	m = typeKeys(m, "/", "a", "u", "t", "h", "enter")
	status := ansi.Strip(renderFilterStatus(m))
	require.Contains(t, status, "done hidden (zh)")
	require.Contains(t, status, "/auth · 2 matches")
	require.Contains(t, status, "+1 in hidden sections (z0)")

	m = typeKeys(m, "esc")
	require.NotContains(t, ansi.Strip(renderFilterStatus(m)), "+1")
}

func TestSearchViewKeepsLayout(t *testing.T) {
	m := searchModel()
	m.Width, m.Height = 100, 24
	m = typeKeys(m, "/", "a", "u", "t", "h", "enter")
	lines := strings.Split(m.View(), "\n")
	require.Len(t, lines, m.Height)
	for i, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), m.Width, "line %d", i)
	}
	require.Contains(t, ansi.Strip(m.View()), "/auth")
}

func TestToggleDoneWhileSearching(t *testing.T) {
	m := doneHeavyModel()
	m = typeKeys(m, "/", "d", "o", "n", "e", "enter")
	require.Len(t, rowIDs(m), 7)
	m = typeKeys(m, "3")
	require.False(t, m.DoneExpanded, "3 only jumps while a search holds Done open")
	require.False(t, doneCollapsed(m))
	require.Equal(t, "done-0", m.SelectedID)
}
