package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// mobileLines is the two mobile lines of the row whose line 1 holds name.
func mobileLines(t *testing.T, m Model, w int, name string) (string, string) {
	t.Helper()
	lines := strings.Split(renderBoard(m, w, 0), "\n")
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), name) && i+1 < len(lines) {
			return ansi.Strip(l), ansi.Strip(lines[i+1])
		}
	}
	t.Fatalf("no row %q", name)
	return "", ""
}

// unseenAsks is an unseen Ready row waiting on your answer.
func unseenAsks(m Model, s protocol.SessionSummary) Model {
	m.EnteredAt = map[string]time.Time{s.ID: mobileNow}
	m.SeenAt = map[string]time.Time{s.ID: mobileNow.Add(-time.Hour)}
	return m
}

// TestMobile_IconsLine1: with row_icons on, line 1 carries the unseen mark
// and the 1-cell why glyph; line 2 keeps the task.
func TestMobile_IconsLine1(t *testing.T) {
	s := protocol.SessionSummary{ID: "1", ShortID: "7d4f", ToolID: "claude", Slug: "dark-mode",
		Title: "pick the theme toggle", State: protocol.StateNeedsInput, WaitReason: protocol.WaitInput,
		LastEventAt: mobileNow.Add(-4 * time.Minute)}
	review := protocol.SessionSummary{ID: "2", ShortID: "b203", ToolID: "claude", Slug: "release-notes",
		Title: "draft the notes", State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview,
		LastEventAt: mobileNow.Add(-11 * time.Minute)}
	m := unseenAsks(mobileModel(t, 50, 24, s, review), s)

	first, second := mobileLines(t, m, 50, "dark-mode")
	require.Equal(t, 50, ansi.StringWidth(first))
	require.Contains(t, first, "✦")
	require.Contains(t, first, "?")
	require.NotContains(t, first, "asks", "the why slot is the glyph only")
	require.True(t, strings.HasSuffix(first, "4m"), "age flush right: %q", first)
	require.Equal(t, 50, ansi.StringWidth(second))
	require.Contains(t, second, "pick the theme toggle", "line 2 keeps the task")

	rFirst, _ := mobileLines(t, m, 50, "release-notes")
	require.NotContains(t, rFirst, "✓", "a finished turn shows no why glyph")
	require.NotContains(t, rFirst, "✦", "seen row: no unseen mark")

	t.Run("ascii glyphs", func(t *testing.T) {
		a := unseenAsks(mobileModel(t, 50, 24, s, review), s)
		require.NoError(t, a.Store.Set("ascii_glyphs", true))
		f, _ := mobileLines(t, a, 50, "dark-mode")
		require.NotContains(t, f, "✦")
		require.Contains(t, f, "* ?")
		r, _ := mobileLines(t, a, 50, "release-notes")
		require.NotContains(t, r, "+")
	})

	t.Run("row_icons off is the old line 1", func(t *testing.T) {
		off := rowIconsOff(mobileModel(t, 50, 24, s, review))
		off = unseenAsks(off, s)
		f, _ := mobileLines(t, off, 50, "dark-mode")
		require.NotContains(t, f, "✦")
		require.NotContains(t, f, "?")
	})

	t.Run("every line exactly the width", func(t *testing.T) {
		for _, w := range []int{20, 30, 35, 36, 40, 50, 60} {
			mm := unseenAsks(mobileModel(t, w, 30, s, review), s)
			for i, l := range strings.Split(renderBoard(mm, w, 0), "\n") {
				if l == "" || strings.Contains(ansi.Strip(l), "nothing") {
					continue
				}
				require.Equal(t, w, ansi.StringWidth(l), "width %d line %d: %q", w, i, ansi.Strip(l))
			}
		}
	})
}

// legendKeys is the plain key and description of each legend row.
func legendKeys(m Model) []string {
	var out []string
	for _, r := range helpLegend(m).Rows {
		out = append(out, ansi.Strip(r.Key)+" "+r.Desc)
	}
	return out
}

// TestLegend_RowIcons: the strip's legend rows follow the subagent row, only
// while row_icons is on; loop and spend rows follow their features.
func TestLegend_RowIcons(t *testing.T) {
	base := []string{
		"✦ finished since you last looked",
		"? asks waiting on your answer",
		"sh 42s waiting on a shell (web, sub)",
	}
	m := helpModel(120, 60)
	require.NoError(t, m.Store.Set("loops", false))
	require.NoError(t, m.Store.Set("usage", false))
	got := legendKeys(m)
	for _, want := range base {
		require.Contains(t, got, want)
	}
	require.NotContains(t, strings.Join(got, "\n"), "⟳ 3/8")
	require.NotContains(t, strings.Join(got, "\n"), "$0.42")
	// Right after the subagent row.
	for i, row := range got {
		if strings.HasPrefix(row, subagentGlyph+" 3") {
			require.Equal(t, base[0], got[i+1])
		}
	}

	require.NoError(t, m.Store.Set("loops", true))
	require.Contains(t, legendKeys(m), "⟳ 3/8 loop turn")
	require.NoError(t, m.Store.Set("usage", true))
	require.Contains(t, legendKeys(m), "$0.42 spend so far (Claude)")

	require.NoError(t, m.Store.Set("ascii_glyphs", true))
	ascii := legendKeys(m)
	for _, want := range []string{"* finished since you last looked", "? asks waiting on your answer",
		"@ 3/8 loop turn", "$0.42 spend so far (Claude)"} {
		require.Contains(t, ascii, want)
	}
	require.NotContains(t, strings.Join(ascii, "\n"), "✦")
	require.NotContains(t, strings.Join(ascii, "\n"), "review", "a finished turn has no row mark (2026-11-12)")

	require.NoError(t, m.Store.Set("row_icons", false))
	off := strings.Join(legendKeys(m), "\n")
	for _, gone := range []string{"finished since you last looked", "asks", "review", "sh 42s", "loop turn", "spend so far"} {
		require.NotContains(t, off, gone)
	}
}
