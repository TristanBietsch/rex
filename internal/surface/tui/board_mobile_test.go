package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var mobileNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// mobileModel is the simple board on a w x h terminal.
func mobileModel(t *testing.T, w, h int, sessions ...protocol.SessionSummary) Model {
	t.Helper()
	m := Model{Sessions: sessions, Filter: "all", Width: w, Height: h, Focus: FocusBoard, Store: settings.NewStore()}
	m.Clock = func() time.Time { return mobileNow }
	if len(sessions) > 0 {
		m.SelectedID = sessions[0].ID
	}
	return m
}

func mobileSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "1", ShortID: "7d4f", ToolID: "claude", Slug: "release-notes", Title: "Which feature should lead: search or export?",
			Description: "draft ready — which feature leads?", State: protocol.StateNeedsInput, Subagents: 2,
			WorkingSince: mobileNow.Add(-45 * time.Minute), LastEventAt: mobileNow.Add(-11 * time.Minute)},
		{ID: "2", ShortID: "b203", ToolID: "claude", Slug: "perf-audit", Title: "events_org_ts index live — p95 38ms",
			State: protocol.StateWorking, LastEventAt: mobileNow.Add(-7 * time.Minute)},
		{ID: "3", ShortID: "3c8a", ToolID: "claude", Slug: "test-coverage", Title: "billing/ from 61% → 92%",
			State: protocol.StateDone, LastEventAt: mobileNow.Add(-2 * time.Hour)},
	}
}

func TestMobileLayoutSetting(t *testing.T) {
	set := func(w int, mode string) Model {
		m := mobileModel(t, w, 20)
		if mode != "" {
			require.NoError(t, m.Store.Set("mobile_layout", mode))
		}
		return m
	}
	require.True(t, mobileLayout(set(60, "")), "auto: 60 columns is mobile")
	require.False(t, mobileLayout(set(61, "")), "auto: 61 is narrow")
	require.False(t, mobileLayout(set(0, "")), "auto: no size yet is not mobile")
	require.True(t, mobileLayout(set(220, "always")))
	require.False(t, mobileLayout(set(40, "off")))
	require.True(t, mobileLayout(Model{Width: 40}), "no store: auto")
	require.Equal(t, classMobile, layoutClassOf(set(50, "")))
	require.Equal(t, classNarrow, layoutClassOf(set(50, "off")), "off falls back to the narrow widths")
	require.Equal(t, classWide, layoutClassOf(set(200, "")))
	require.Equal(t, 2, rowLines(set(40, "")))
	require.Equal(t, 1, rowLines(set(80, "")))
}

func TestMobileTwoLineRows(t *testing.T) {
	m := rowIconsOff(mobileModel(t, 40, 24, mobileSessions()...)) // pins the line 1 without the strip
	lines := strings.Split(renderBoard(m, 40, 0), "\n")
	var first, second string
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "release-notes") {
			first, second = ansi.Strip(l), ansi.Strip(lines[i+1])
		}
	}
	require.NotEmpty(t, first)
	require.Equal(t, 40, ansi.StringWidth(first))
	require.True(t, strings.HasPrefix(first, " ▸  ⚑  release-notes"), "line 1: %q", first)
	require.Contains(t, first, subagentGlyph+" 2")
	require.True(t, strings.HasSuffix(first, "11m"), "age flush right: %q", first)
	require.Equal(t, 40, ansi.StringWidth(second))
	require.True(t, strings.HasPrefix(second, "       Which feature should lead"), "line 2: %q", second)
	require.Contains(t, second, "…", "the text is cut to the width")

	// Every row is two lines, so a board of 3 rows has 6 row lines.
	slots := boardSlots(m)
	rows, conts := 0, 0
	for i, sl := range slots {
		switch sl.kind {
		case slotRow:
			rows++
			require.Equal(t, slotRowCont, slots[i+1].kind, "a continuation follows every row")
			require.Equal(t, sl.sess.ID, slots[i+1].sess.ID)
		case slotRowCont:
			conts++
		default: // titles, gaps, empty columns
		}
	}
	require.Equal(t, 3, rows)
	require.Equal(t, 3, conts)

	t.Run("detailed line 2 is the summary", func(t *testing.T) {
		d := m
		d.Detailed = true
		out := ansi.Strip(renderBoard(d, 40, 0))
		require.Contains(t, out, "       draft ready — which feature")
	})
	t.Run("subagent and badge cells only fit from 36 columns", func(t *testing.T) {
		for _, w := range []int{35, 36} {
			narrow := mobileModel(t, w, 24, mobileSessions()...)
			out := ansi.Strip(renderBoard(narrow, w, 0))
			require.Equal(t, w >= 36, strings.Contains(out, subagentGlyph), "width %d", w)
		}
	})
}

func TestMobileRowsFillWidthExactly(t *testing.T) {
	for _, detailed := range []bool{false, true} {
		for _, w := range []int{20, 30, 40, 45, 60} {
			m := mobileModel(t, w, 30, mobileSessions()...)
			m.Detailed = detailed
			for i, l := range strings.Split(renderBoard(m, w, 0), "\n") {
				if l == "" || strings.Contains(ansi.Strip(l), "nothing") {
					continue // a gap between sections, or an empty column's line
				}
				require.Equal(t, w, ansi.StringWidth(l), "detailed=%v width %d line %d: %q", detailed, w, i, ansi.Strip(l))
			}
		}
	}
}

func TestMobileClickSecondLineSelects(t *testing.T) {
	m := mobileModel(t, 40, 24, mobileSessions()...)
	m.SelectedID = "3"
	slots := boardSlots(m)
	first, cont := -1, -1
	for i, sl := range slots {
		if sl.sess.ID != "2" {
			continue
		}
		switch sl.kind {
		case slotRow:
			first = i
		case slotRowCont:
			cont = i
		default:
		}
	}
	require.Equal(t, first+1, cont)
	for _, line := range []int{first, cont} {
		y := boardTop(m) + line - m.ScrollOffset
		require.Equal(t, "2", sessionAtScreenRow(m, y), "line %d", line)
		got, _ := updateBoardMouse(m, click(5, y))
		require.Equal(t, "2", got.SelectedID)
	}
}

func TestMobileEnsureVisibleKeepsBothLines(t *testing.T) {
	var sessions []protocol.SessionSummary
	for i := 0; i < 12; i++ {
		sessions = append(sessions, protocol.SessionSummary{ID: fmt.Sprintf("s%02d", i), Slug: fmt.Sprintf("job-%02d", i),
			Title: fmt.Sprintf("task number %02d", i), State: protocol.StateWorking, LastEventAt: mobileNow})
	}
	m := mobileModel(t, 40, 16, sessions...)
	for range sessions {
		m = moveSelection(m, 1)
		var text bool
		for _, l := range strings.Split(ansi.Strip(renderFullScreen(m, 40, 16)), "\n") {
			text = text || strings.HasPrefix(l, "       task number "+m.SelectedID[1:])
		}
		require.True(t, text, "selected %s: both lines on screen (scroll %d)", m.SelectedID, m.ScrollOffset)
	}
	for i := len(sessions) - 1; i >= 0; i-- {
		m = moveSelection(m, -1)
		out := ansi.Strip(renderFullScreen(m, 40, 16))
		require.Contains(t, out, "task number "+m.SelectedID[1:], "up: selected %s scroll %d", m.SelectedID, m.ScrollOffset)
	}
}

// No line of the whole screen is wider than the terminal, in either view
// and in every state that adds header or help text.
func TestMobileHeaderHelpFit40x20(t *testing.T) {
	for _, detailed := range []bool{false, true} {
		m := mobileModel(t, 40, 20, mobileSessions()...)
		m.Detailed = detailed
		m.BackendUnavailable = true
		m.BackendUnavailableReason = "ollama unreachable at http://localhost:11434"
		require.NoError(t, m.Store.Set("overview_enabled", true))
		m.Sessions[0].RetireAt = mobileNow.Add(time.Hour) // a "marked done" header suffix
		for i, l := range strings.Split(renderFullScreen(m, 40, 20), "\n") {
			require.LessOrEqual(t, ansi.StringWidth(l), 40, "detailed=%v line %d: %q", detailed, i, ansi.Strip(l))
		}
		top := ansi.Strip(strings.Split(renderHeader(m, 40), "\n")[0])
		require.NotContains(t, top, "detailed (v)")
		require.NotContains(t, top, "need you", "mobile always uses the glyph counts")
		require.Contains(t, top, "⚑")
		help := ansi.Strip(renderHelpLine(m, 40))
		require.Contains(t, help, "enter open")
		require.Contains(t, help, "i new")
		require.Contains(t, help, "? ")
		require.NotContains(t, help, "tab")
		require.NotContains(t, help, "cc")
	}
}

func TestMobilePreviewLineOff(t *testing.T) {
	m := mobileModel(t, 40, 30, mobileSessions()...)
	require.Equal(t, 0, previewRows(m))
	require.Equal(t, baseBottomRows, boardBottomRows(m))
	m.Width = 80
	require.Equal(t, 1, previewRows(m), "the desktop layout keeps its preview line")
}

func TestResizeAcrossBreakpointKeepsSelection(t *testing.T) {
	var sessions []protocol.SessionSummary
	for i := 0; i < 15; i++ {
		sessions = append(sessions, protocol.SessionSummary{ID: fmt.Sprintf("s%02d", i), Slug: fmt.Sprintf("job-%02d", i),
			Title: fmt.Sprintf("task number %02d", i), State: protocol.StateWorking, LastEventAt: mobileNow})
	}
	m := mobileModel(t, 80, 20, sessions...)
	m.SelectedID = "s11"
	m = ensureVisible(m)
	for _, w := range []int{40, 80, 55, 61, 60, 150, 30} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		m = next.(Model)
		require.Equal(t, "s11", m.SelectedID, "width %d", w)
		out := ansi.Strip(renderFullScreen(m, w, 20))
		require.Contains(t, out, "▸ ", "width %d", w)
		require.Contains(t, out, "job-11", "width %d: the selection is on screen", w)
	}
}

// Whatever the width, mode and view, the whole screen stays inside the
// terminal and keeps its height.
func TestScreenFitsEveryWidth(t *testing.T) {
	for _, fit := range []bool{false, true} {
		for _, detailed := range []bool{false, true} {
			for _, w := range layoutWidths {
				m := layoutModel(mobileNow, w, layoutSessions(mobileNow)...)
				m.Height = 24
				m.Detailed = detailed
				if fit {
					m = fitModel(t, m)
				}
				lines := strings.Split(renderFullScreen(m, w, 24), "\n")
				require.Len(t, lines, 24, "fit=%v detailed=%v width %d", fit, detailed, w)
				for i, l := range lines {
					require.LessOrEqual(t, ansi.StringWidth(l), w, "fit=%v detailed=%v width %d line %d: %q", fit, detailed, w, i, ansi.Strip(l))
				}
			}
		}
	}
}
