package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func scrollModel(t *testing.T, n int, density string, height int) Model {
	t.Helper()
	m := benchModel(n)
	m.Height = height
	m.Focus = FocusBoard
	require.NoError(t, m.Store.Set("row_density", density))
	return m
}

// arrowVisible reports whether the full screen shows the selection arrow on
// the selected session's row.
func arrowVisible(m Model) bool {
	for _, line := range strings.Split(renderFullScreen(m, m.Width, m.Height), "\n") {
		if strings.Contains(line, "▸") {
			for _, s := range m.Sessions {
				if s.ID == m.SelectedID {
					return strings.Contains(line, s.Slug)
				}
			}
		}
	}
	return false
}

// Regression: with row_density roomy the renderer inserted 2 blank lines
// between sections while the scroll math assumed 1, so scrolling down left
// the selected row (and its ▸) just below the window.
func TestScroll_ArrowStaysVisibleAllDensities(t *testing.T) {
	for _, density := range []string{"compact", "normal", "roomy"} {
		t.Run(density, func(t *testing.T) {
			m := scrollModel(t, 30, density, 20)
			rows := orderedSessions(m)
			m = moveSelection(m, 1)
			for i := 0; i < len(rows)+2; i++ {
				require.True(t, arrowVisible(m), "down step %d: selected %s scroll %d", i, m.SelectedID, m.ScrollOffset)
				m = moveSelection(m, 1)
			}
			for i := 0; i < len(rows)+2; i++ {
				require.True(t, arrowVisible(m), "up step %d: selected %s scroll %d", i, m.SelectedID, m.ScrollOffset)
				m = moveSelection(m, -1)
			}
		})
	}
}

func TestScroll_JumpToLastWithBanner(t *testing.T) {
	m := scrollModel(t, 30, "roomy", 18)
	m.BackendUnavailable = true // header grows a line
	rows := orderedSessions(m)
	m.SelectedID = rows[len(rows)-1].ID
	m = ensureVisible(m)
	require.True(t, arrowVisible(m), "scroll %d", m.ScrollOffset)
}

// A session moving to another section (working → done) must not strand the
// selection off-screen.
func TestScroll_SelectionFollowsStateChange(t *testing.T) {
	m := scrollModel(t, 30, "normal", 20)
	rows := orderedSessions(m)
	m.SelectedID = rows[0].ID // top of Needs input
	m = ensureVisible(m)
	for i := range m.Sessions {
		if m.Sessions[i].ID == m.SelectedID {
			m.Sessions[i].State = protocol.StateDone // jumps to bottom section
		}
	}
	m = ensureVisible(m)
	require.True(t, arrowVisible(m), "scroll %d", m.ScrollOffset)
}

func TestScroll_OffsetClampedToContent(t *testing.T) {
	m := scrollModel(t, 3, "normal", 30)
	m.ScrollOffset = 50
	m = ensureVisible(m)
	require.Equal(t, 0, m.ScrollOffset)
}

// Mouse hit-testing uses the same layout + scroll as rendering.
func TestMouse_ClickMapsToRenderedRow(t *testing.T) {
	m := scrollModel(t, 30, "roomy", 20)
	rows := orderedSessions(m)
	m.SelectedID = rows[len(rows)-1].ID
	m = ensureVisible(m)
	require.NotZero(t, m.ScrollOffset)

	screen := strings.Split(renderFullScreen(m, m.Width, m.Height), "\n")
	for y, line := range screen {
		id := sessionAtScreenRow(m, y)
		if id == "" {
			continue
		}
		var slug string
		for _, s := range m.Sessions {
			if s.ID == id {
				slug = s.Slug
			}
		}
		require.Contains(t, line, slug, "row %d", y)
	}
}

func TestTruncate_WideRunesFitColumns(t *testing.T) {
	got := truncate("修复认证错误并添加测试", 9)
	require.LessOrEqual(t, ansi.StringWidth(got), 9)
	require.True(t, strings.HasSuffix(got, "…"))
	require.Equal(t, "short", truncate("short", 9))
}
