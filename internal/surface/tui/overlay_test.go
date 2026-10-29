package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// popupLeftEdge returns the column of the popup's left border on a line, or
// -1 when the line has none.
func popupLeftEdge(line string) int {
	plain := ansi.Strip(line)
	i := strings.IndexAny(plain, "│╭╰")
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(plain[:i])
}

func TestCenterOverlay_WideBoardRunesKeepColumns(t *testing.T) {
	// 60 columns of wide runes: with an odd popup x every edge straddles one.
	bg := strings.TrimSuffix(strings.Repeat(strings.Repeat("日", 30)+"\n", 7), "\n")
	for _, w := range []int{60, 61, 62, 63} {
		out := centerOverlay(w, 7, "x", bg)
		lines := strings.Split(out, "\n")
		require.Len(t, lines, 7)
		edge := -1
		for i, l := range lines {
			require.Equal(t, 60, ansi.StringWidth(l), "w=%d line %d %q", w, i, ansi.Strip(l))
			if e := popupLeftEdge(l); e >= 0 {
				if edge < 0 {
					edge = e
				}
				require.Equal(t, edge, e, "w=%d line %d: popup row shifted", w, i)
			}
		}
		require.GreaterOrEqual(t, edge, 0, "w=%d: popup not drawn", w)
	}
}

func TestCenterOverlay_ClipsPopupWiderThanScreen(t *testing.T) {
	content := strings.Repeat("y", 100)
	out := centerOverlay(40, 5, content, "")
	for i, l := range strings.Split(out, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(l), 40, "line %d", i)
	}
}
