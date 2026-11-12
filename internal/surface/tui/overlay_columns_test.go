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
	i := strings.Index(plain, "│")
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(plain[:i])
}

// Over a board of wide runes every popup edge can straddle one: each line
// must stay exactly the board's width and the popup must not zig-zag.
func TestCompositeOverlay_WideBoardRunesKeepColumns(t *testing.T) {
	bg := strings.TrimSuffix(strings.Repeat(strings.Repeat("日", 30)+"\n", 7), "\n")
	popup := []string{"│x│", "│y│", "│z│"}
	for _, w := range []int{60, 61, 62, 63} {
		lines := strings.Split(compositeOverlay(w, 7, popup, 3, bg), "\n")
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

// A popup wider than the screen is clipped to it.
func TestCompositeOverlay_ClipsPopupWiderThanScreen(t *testing.T) {
	out := compositeOverlay(40, 3, []string{strings.Repeat("y", 100)}, 100, "")
	for i, l := range strings.Split(out, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(l), 40, "line %d", i)
	}
}
