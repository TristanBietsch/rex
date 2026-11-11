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

const longDesc = "Running pnpm test:billing after the rounding fix — 3 failures left in invoice.spec.ts; the currency fixtures are next"

var previewNow = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func previewModel(h int, sessions ...protocol.SessionSummary) Model {
	m := boardModel(sessions...)
	m.Width, m.Height = 100, h
	m.Store = settings.NewStore()
	m.Clock = func() time.Time { return previewNow }
	return m
}

func described(id, desc string) protocol.SessionSummary {
	return protocol.SessionSummary{ID: id, ShortID: id, Slug: id, Title: "fix invoice rounding", State: protocol.StateWorking,
		Description: desc, DescriptionAt: previewNow.Add(-12 * time.Second), LastEventAt: previewNow}
}

func plain(m Model, h int) []string {
	return strings.Split(ansi.Strip(renderFullScreen(m, m.Width, h)), "\n")
}

func previewLine(lines []string) string {
	for _, l := range lines {
		if strings.Contains(l, "↳") {
			return l
		}
	}
	return ""
}

func TestPreview_ShownForSelectedSummary_SimpleView(t *testing.T) {
	m := previewModel(30, described("a", longDesc), described("b", "Editing x.go"))
	lines := plain(m, 30)
	require.Len(t, lines, 30)
	got := previewLine(lines)
	require.Contains(t, got, "↳ Running pnpm test:billing")
	require.Contains(t, got, "· 12s", "age from description_at")

	m.SelectedID = "b"
	require.Contains(t, previewLine(plain(m, 30)), "↳ Editing x.go · 12s")
}

func TestPreview_HiddenWhenFitsDetailed(t *testing.T) {
	m := previewModel(30, described("a", "Editing x.go"))
	m.Detailed = true
	require.Empty(t, previewLine(plain(m, 30)), "the row already shows all of it")
	require.Equal(t, 1, previewRows(m), "the line stays reserved so the board does not jump")

	m.Sessions = []protocol.SessionSummary{described("a", longDesc)}
	m.Width = 80
	require.Contains(t, previewLine(plain(m, 30)), "↳ Running pnpm test:billing", "cut in its row: preview shows it all")
}

func TestPreview_RowReservationRules(t *testing.T) {
	long := previewModel(40, described("a", longDesc))
	require.NoError(t, long.Store.Set("summary_detail", "long"))
	require.Equal(t, 2, previewRows(long), "tall terminal, long detail")
	long.Height = 25
	require.Equal(t, 1, previewRows(long), "not tall enough for two")
	long.Height = 19
	require.Equal(t, 0, previewRows(long), "too short for any")

	none := previewModel(40, protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	require.Equal(t, 0, previewRows(none), "D3: only while some session has a description")
	require.Equal(t, 3, boardBottomRows(none))

	one := previewModel(40, described("a", "x"), protocol.SessionSummary{ID: "b", Slug: "b"})
	require.Equal(t, 4, boardBottomRows(one))
}

func TestPreview_TwoLinesWrapAndCut(t *testing.T) {
	m := previewModel(40, described("a", strings.Repeat("alpha beta gamma ", 12)))
	require.NoError(t, m.Store.Set("summary_detail", "long"))
	m.Width = 61 // 60 and below is the mobile layout, which has no preview line
	lines := plain(m, 40)
	require.Len(t, lines, 40)
	var rows []string
	for i, l := range lines {
		if strings.Contains(l, "↳") {
			rows = lines[i : i+2]
		}
	}
	require.Len(t, rows, 2)
	require.True(t, strings.HasPrefix(rows[1], "    alpha") || strings.HasPrefix(rows[1], "    beta") || strings.HasPrefix(rows[1], "    gamma"), rows[1])
	require.Contains(t, rows[1], "…", "text past two lines is cut")
	for _, l := range rows {
		require.LessOrEqual(t, ansi.StringWidth(l), contentWidth(m.Width))
	}
}

func TestBoardHeight_StableAcrossSelection(t *testing.T) {
	m := previewModel(30, described("a", longDesc), protocol.SessionSummary{ID: "b", Slug: "b", State: protocol.StateWorking})
	h1 := boardHeight(m)
	m.SelectedID = "b" // no description: the preview line is blank, not gone
	require.Equal(t, h1, boardHeight(m))
	require.Len(t, plain(m, 30), 30)
	require.Equal(t, 27-boardTop(m)-1, h1-0, "board loses exactly the one preview row")
}

func TestMouse_PreviewRowsHitNothing(t *testing.T) {
	m := previewModel(30, described("a", longDesc))
	bh := boardHeight(m)
	top := boardTop(m)
	rowY := -1
	for y := top; y < top+bh; y++ {
		if sessionAtScreenRow(m, y) == "a" {
			rowY = y
		}
	}
	require.GreaterOrEqual(t, rowY, top, "the session row is found inside the board")
	for y := top + bh; y < 30; y++ {
		require.Empty(t, sessionAtScreenRow(m, y), "row %d is preview or prompt", y)
	}
}

func TestPreview_OffIsInert(t *testing.T) {
	m := previewModel(30, described("a", longDesc))
	require.NoError(t, m.Store.Set(featureSummaryPreview, false))
	require.Equal(t, 0, previewRows(m))
	require.Equal(t, 3, boardBottomRows(m), "row accounting as before")
	lines := plain(m, 30)
	require.Len(t, lines, 30)
	require.Empty(t, previewLine(lines))
	require.Empty(t, renderDescPreview(m, 80))

	on := previewModel(30, described("a", longDesc))
	require.Equal(t, boardHeightFor(m, 30)-1, boardHeightFor(on, 30))
}

func TestPreview_SnapshotWidths(t *testing.T) {
	for _, w := range []int{80, 140} {
		m := previewModel(30, described("a", longDesc))
		m.Width = w
		lines := plain(m, 30)
		require.Len(t, lines, 30, "width %d", w)
		got := previewLine(lines)
		require.NotEmpty(t, got, "width %d", w)
		require.LessOrEqual(t, ansi.StringWidth(got), contentWidth(w), "width %d", w)
		// Directly under the board, directly above the bottom rule.
		i := 0
		for j, l := range lines {
			if strings.Contains(l, "↳") {
				i = j
			}
		}
		require.Equal(t, 30-baseBottomRows-1, i, "width %d", w)
	}
}
