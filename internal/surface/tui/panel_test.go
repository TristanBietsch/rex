package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func TestPanelStyleSquareBorderExactWidth(t *testing.T) {
	out := panelStyle(40).Render("hello")
	if !strings.Contains(out, "┌") || strings.Contains(out, "╭") {
		t.Fatalf("expected square border:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w != 40 {
			t.Fatalf("line width %d != 40: %q", w, l)
		}
	}
}

func TestSectionHeaderWidth(t *testing.T) {
	for _, w := range []int{20, 40, 80} {
		if got := lipgloss.Width(sectionHeader("Keys", w)); got != w {
			t.Fatalf("width %d: got %d", w, got)
		}
	}
}

func TestPanelWidth(t *testing.T) {
	cases := []struct{ term, want, min, exp int }{
		{200, 80, 40, 80},
		{60, 80, 40, 56},
		{30, 80, 40, 28}, // the floor gives way: never wider than term-2
	}
	for _, c := range cases {
		if got := panelWidth(c.term, c.want, c.min); got != c.exp {
			t.Fatalf("panelWidth(%d,%d,%d)=%d want %d", c.term, c.want, c.min, got, c.exp)
		}
	}
}

func TestPanelTitleInTopBorder(t *testing.T) {
	lines, w := panelLines("HELP", "rex v1", []string{"hello", "wider line here"}, 3, 0, 0)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
		require.Equal(t, w, ansi.StringWidth(l), "line %d", i)
	}
	require.Len(t, lines, 4, "top border, two body rows, bottom border")
	require.True(t, strings.HasPrefix(plain[0], "┌─ HELP "), plain[0])
	require.True(t, strings.HasSuffix(plain[0], " rex v1 ─┐"), plain[0])
	require.Equal(t, "│   hello             │", plain[1])
	require.True(t, strings.HasPrefix(plain[3], "└─") && strings.HasSuffix(plain[3], "─┘"), plain[3])
}

func TestPanelFixedWidthAndPadding(t *testing.T) {
	lines, w := panelLines("T", "", []string{"x"}, 2, 1, 30)
	require.Equal(t, 30, w)
	require.Len(t, lines, 5, "border, pad row, body, pad row, border")
	for _, l := range lines {
		require.Equal(t, 30, ansi.StringWidth(l))
	}
}

func TestPanelNarrowKeepsBoxIntact(t *testing.T) {
	for width := 4; width <= 24; width++ {
		lines, w := panelLines("A LONG TITLE", "a long right label", []string{"body text that is long"}, 1, 0, width)
		require.Equal(t, width, w)
		for _, l := range lines {
			require.Equal(t, width, ansi.StringWidth(l), "width %d: %q", width, ansi.Strip(l))
		}
		top := ansi.Strip(lines[0])
		require.True(t, strings.HasPrefix(top, "┌") && strings.HasSuffix(top, "┐"), top)
	}
}

// Every full-screen overlay wears the same chrome: a square box with its
// title set into the top edge.
func TestOverlaysShareTitledPanelChrome(t *testing.T) {
	titles := map[string]string{
		"help": "HELP", "settings": "SETTINGS", "wizard": "which provider?",
		"stats": "Session Stats", "overview": "Overview", "failures": "failed sessions",
	}
	for name, m := range overlayModels(t, 100, 50) {
		plain := ansi.Strip(m.View())
		require.NotContains(t, plain, "╭", name)
		require.Regexp(t, "┌─ "+regexp.QuoteMeta(titles[name])+" ", plain, name)
		require.Contains(t, plain, "┘", name)
	}
}

// The board behind a non-attach overlay is dimmed to the muted color, with
// no other styling left on it.
func TestBoardDimmedBehindOverlay(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		muted, _, _ := strings.Cut(styleMuted.Render("x"), "x") // the muted SGR
		for name, m := range overlayModels(t, 120, 40) {
			styled := 0
			for i, line := range strings.Split(m.View(), "\n") {
				if strings.TrimSpace(ansi.Strip(ansi.Truncate(line, 3, ""))) == "" {
					continue // blank margin: nothing to color
				}
				styled++
				require.True(t, strings.HasPrefix(line, muted), "%s row %d: %q", name, i, line)
			}
			require.Positive(t, styled, name)
		}
	})
}

// The attach popup is not dimmed: it redraws every frame, and the board
// around it keeps its colors.
func TestAttachDoesNotDimBoard(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		m := bannerModel(t, 120, 40, bannerSession())
		header := strings.Split(m.View(), "\n")[1]
		require.NotEqual(t, styleMuted.Render(ansi.Strip(header)), header)
		require.Contains(t, header, styleLogo.Render("∴ REX"))
	})
}
