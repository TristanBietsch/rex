package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

// pageHelp is the help page (fullscreen_pages on, the default) at w x h.
func pageHelp(t *testing.T, w, h int) Model {
	t.Helper()
	m := helpModel(w, h)
	require.True(t, pagesOn(m))
	return m
}

// helpPagePress presses keys on the help page (tab and shift+tab as their
// key types).
func helpPagePress(m Model, keys ...string) Model { return settingsPress(m, keys...) }

// rightPane is the text right of the pane bar on each body row.
func rightPane(m Model) string {
	var out []string
	for _, l := range pageView(m) {
		if _, r, ok := strings.Cut(l, " │ "); ok {
			out = append(out, r)
		}
	}
	return strings.Join(out, "\n")
}

func TestHelpPageEveryGroupReachable(t *testing.T) {
	m := pageHelp(t, 120, 40)
	groups := helpGroups(m)
	require.Len(t, groups, 9)
	titles := make([]string, len(groups))
	for i, g := range groups {
		titles[i] = g.Title
	}
	require.Equal(t, []string{"Navigation", "Filter & search", "Actions", "Mouse", "Modes", "Legend",
		"Session commands", "App commands", "Attached session"}, titles)
	for i, g := range groups {
		m = helpPagePress(m, strconv.Itoa(i+1))
		require.Equal(t, i, m.Help.Group)
		right := rightPane(m)
		require.Contains(t, right, g.Title, "group %d", i+1)
		require.Contains(t, right, g.Rows[0].Desc, "group %q first row", g.Title)
	}
}

// longGroup is the first group taller than the help page at w x h.
func longGroup(t *testing.T, w, h int) int {
	t.Helper()
	m := pageHelp(t, w, h)
	for i := range helpGroups(m) {
		m.Help.Group = i
		if helpLayoutFor(m, w, h).maxScroll > 0 {
			return i
		}
	}
	t.Fatalf("no group scrolls at %dx%d", w, h)
	return 0
}

func TestHelpPageScrollResetsOnGroup(t *testing.T) {
	m := pageHelp(t, 80, 24)
	g := longGroup(t, 80, 24)
	m = navPress(m, strconv.Itoa(g+1), "ctrl+f")
	require.Positive(t, m.Help.Scroll, "group %d is longer than the page at 80x24", g+1)
	m = helpPagePress(m, "]")
	require.Equal(t, g+1, m.Help.Group)
	require.Zero(t, m.Help.Scroll)

	m = navPress(m, "[", "ctrl+f", "9")
	require.Zero(t, m.Help.Scroll, "1-9 also resets the scroll")
	m = helpPagePress(m, "]")
	require.Equal(t, 8, m.Help.Group, "] clamps at the last group")
	m = helpPagePress(m, "1", "[")
	require.Zero(t, m.Help.Group, "[ clamps at the first group")
}

func TestHelpPageLeftPaneKeys(t *testing.T) {
	m := pageHelp(t, 120, 40)
	m = helpPagePress(m, "tab")
	require.Equal(t, paneSections, m.Help.Pane)
	m = helpPagePress(m, "k")
	require.Zero(t, m.Help.Group, "k clamps at the first group")
	m = helpPagePress(m, "j", "j")
	require.Equal(t, 2, m.Help.Group)
	m = helpPagePress(m, "9", "j")
	require.Equal(t, 8, m.Help.Group, "j clamps at the last group")
	m = helpPagePress(m, "l")
	require.Equal(t, paneRows, m.Help.Pane)
	m = helpPagePress(m, "shift+tab", "enter")
	require.Equal(t, paneRows, m.Help.Pane)
	m = helpPagePress(m, "tab", "/")
	require.True(t, m.Help.Filtering)
	require.Equal(t, paneRows, m.Help.Pane, "/ focuses the rows")
}

func TestHelpPageFilterSpansGroups(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		m := pageHelp(t, 120, 40)
		m = helpPagePress(m, "/", "u", "n", "d", "o", "enter")
		right := rightPane(m)
		hit := 0
		for _, g := range helpGroups(m) {
			if strings.Contains(right, "── "+g.Title) {
				hit++
			}
		}
		require.GreaterOrEqual(t, hit, 2, "the hits come from several groups:\n%s", right)
		require.Contains(t, m.View(), styleMuted.Render("Mouse"), "a group with no hits is muted")
	})
}

func TestHelpPageFilterEmpty(t *testing.T) {
	m := helpPagePress(pageHelp(t, 120, 40), "/", "z", "z", "z", "z")
	require.Contains(t, rightPane(m), "no match for “zzzz”")
}

func TestHelpPageOffRowsDimmed(t *testing.T) {
	m := pageHelp(t, 120, 40)
	require.NoError(t, m.Store.Set("undo_complete", false))
	gi := -1
	for i, g := range helpGroups(m) {
		for _, r := range g.Rows {
			if strings.HasPrefix(r.Key, ":uc") {
				gi = i
			}
		}
	}
	require.GreaterOrEqual(t, gi, 0, "a group lists :uc")
	m = helpPagePress(m, strconv.Itoa(gi+1))
	found := false
	for _, l := range strings.Split(rightPane(m), "\n") {
		if strings.Contains(l, ":uc") {
			require.Contains(t, l, "(off)")
			found = true
		}
	}
	require.True(t, found, ":uc is on the page")
}

func TestHelpPageLegendSpinner(t *testing.T) {
	m := pageHelp(t, 120, 40)
	m = helpPagePress(m, "6")
	require.Equal(t, helpGroupLegend, helpGroups(m)[m.Help.Group].Title)
	m.SpinnerTick = 0
	a := m.View()
	m.SpinnerTick = 1
	require.NotEqual(t, a, m.View(), "the legend's working marker spins")
}

func TestHelpPageNarrow(t *testing.T) {
	m := pageHelp(t, 50, 20)
	text := ansi.Strip(m.View())
	require.Contains(t, text, "‹ Navigation ›")
	require.NotContains(t, text, " │ ")
	require.NotContains(t, text, "tab groups", "tab does nothing here")
	m = helpPagePress(m, "tab")
	require.Equal(t, paneRows, m.Help.Pane, "tab is a no-op on a narrow page")

	m = helpPagePress(m, "]")
	require.Contains(t, ansi.Strip(m.View()), "‹ Filter & search ›")

	// A section-pane focus left over from a wider page acts as rows.
	g := longGroup(t, 50, 20)
	m = helpPagePress(m, strconv.Itoa(g+1))
	m.Help.Pane = paneSections
	m = helpPagePress(m, "j")
	require.Equal(t, g, m.Help.Group)
	require.Equal(t, 1, m.Help.Scroll, "j scrolls")
}

func TestHelpPageWheel(t *testing.T) {
	m := pageHelp(t, 80, 24)
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	m, _ = updateMouse(m, wheelAt(true, 4, 8))
	require.Equal(t, 1, m.Help.Group, "wheel over the groups: next group")
	require.Zero(t, m.Help.Scroll)

	g := longGroup(t, 80, 24)
	m = helpPagePress(m, strconv.Itoa(g+1))
	m, _ = updateMouse(m, wheelAt(true, 60, 8))
	require.Equal(t, 1, m.Help.Scroll, "wheel over the rows scrolls one row, as j")
	require.Equal(t, g, m.Help.Group)
}

func TestHelpPageMaxScrollIsRightPane(t *testing.T) {
	m := pageHelp(t, 120, 40)
	m = navPress(m, "2", "ctrl+f")
	require.Zero(t, m.Help.Scroll, "Filter fits the page; the other groups do not count")
}

func TestHelpOverlayKeysUnchanged(t *testing.T) {
	m := overlayMode(t, helpModel(120, 40))
	before := *m.Help
	m = helpPagePress(m, "1", "]", "tab", "q")
	require.Equal(t, FocusHelp, m.Focus, "q does not close the overlay")
	require.Equal(t, before, *m.Help)
}

func TestHelpPageClose(t *testing.T) {
	m := helpPagePress(pageHelp(t, 120, 40), "/", "u", "n", "d", "o", "enter", "2")
	m = helpPagePress(m, "esc")
	require.Equal(t, FocusHelp, m.Focus, "first esc clears the filter")
	require.Empty(t, m.Help.Filter)
	require.Equal(t, 1, m.Help.Group, "and keeps the group")
	m = helpPagePress(m, "esc")
	require.Equal(t, FocusBoard, m.Focus, "second esc closes")

	for _, k := range []string{"q", "?"} {
		m = helpPagePress(pageHelp(t, 120, 40), "tab", k)
		require.Equal(t, FocusBoard, m.Focus, "%s closes from the group pane", k)
	}
}
