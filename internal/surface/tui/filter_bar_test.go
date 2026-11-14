package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestRenderFilterStatus(t *testing.T) {
	m := filterModel()
	require.Empty(t, renderFilterStatus(m))

	one := setSectionHidden(m, "done", true)
	require.Equal(t, "done hidden (zh)", ansi.Strip(renderFilterStatus(one)))

	working := setSectionHidden(m, "working", true)
	require.Equal(t, "working hidden (z3)", ansi.Strip(renderFilterStatus(working)))

	two := setSectionHidden(one, "needs", true)
	require.Equal(t, "needs, done hidden (z0 shows all)", ansi.Strip(renderFilterStatus(two)))
}

// One tool on the board: the bar still shows while a section is hidden, so
// the hidden state is visible and clearable.
func TestFilterBarShowsForHiddenSections(t *testing.T) {
	one := simpleBoard(
		protocol.SessionSummary{ID: "a", ToolID: "claude", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "b", ToolID: "claude", State: protocol.StateDone},
	)
	require.False(t, showFilterChips(one))

	hid := setSectionHidden(one, "done", true)
	require.True(t, showFilterChips(hid))
	require.Equal(t, 2, headerHeight(hid))
	require.Contains(t, ansi.Strip(renderHeader(hid, 120)), "done hidden (zh)")
}

// Hiding Done keeps the screen at its exact size, nothing over-wide, the
// header names the hidden section and the board has no DONE title.
func TestBoardHideDoneView(t *testing.T) {
	for _, tc := range []struct {
		name   string
		line   string
		hidden string // title expected in the header
		absent string // title missing from the board
	}{
		{"board-hide-done", "filter hide done", "done hidden", "DONE"},
		{"board-only-needs", "filter only needs", "working, done hidden", "WORKING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := filterModel()
			m.Width, m.Height = 100, 24
			m, _ = executeCommand(m, tc.line)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			require.Len(t, lines, m.Height)
			for i, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), m.Width, "line %d: %q", i, l)
			}
			screen := strings.Join(lines, "\n")
			require.Contains(t, lines[2], tc.hidden, "filter bar, screen row 2")
			require.NotContains(t, strings.Join(lines[boardTop(m):], "\n"), tc.absent)
			require.Contains(t, screen, "READY")
		})
	}
}

// :prune clears every Done session whatever the filters show; the confirm
// line says so only while a filter is active.
func TestPruneConfirmSaysFiltersIgnored(t *testing.T) {
	m := boardModel(pruneFixture()...)
	m = typeCommand(m, "prune")
	require.NotContains(t, renderDeleteConfirm(m, 300), "filters ignored")

	m = boardModel(pruneFixture()...)
	m = setSectionHidden(m, "working", true)
	m = typeCommand(m, "prune")
	require.Contains(t, renderDeleteConfirm(m, 300), "clear 4 completed (all Done sessions, filters ignored)?")
}

// Every tool chip still hits with a hidden-section suffix on the bar.
func TestBoardMouse_FilterChipClickWithHiddenSections(t *testing.T) {
	m := scrollModel(t, 30, "normal", 30)
	m = setSectionHidden(m, "done", true)
	require.Contains(t, ansi.Strip(renderHeader(m, contentWidth(m.Width))), "done hidden")
	for _, tool := range []string{"claude", "codex", "antigravity", "ollama", "all"} {
		x := headerX(t, m, 2, " "+tool+" ") + 1
		got, _ := updateMouse(m, click(x, 2))
		require.Equal(t, tool, got.Filter, "click %q", tool)
		require.True(t, got.Hidden["done"], "a chip click keeps hidden sections")
	}
}
