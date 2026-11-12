package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var sgrSeq = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// paintsBackground reports whether any SGR sequence in s sets a background
// color (40-47, 48;…, 100-107) or reverse video (7). Color arguments of
// 38/48 are skipped, so a foreground component never reads as a code.
func paintsBackground(s string) bool {
	for _, m := range sgrSeq.FindAllStringSubmatch(s, -1) {
		ps := strings.Split(m[1], ";")
		for i := 0; i < len(ps); i++ {
			n, _ := strconv.Atoi(ps[i])
			switch {
			case n == 38 && i+1 < len(ps) && ps[i+1] == "5":
				i += 2
			case n == 38 && i+1 < len(ps) && ps[i+1] == "2":
				i += 4
			case n == 48, n == 7, n >= 40 && n <= 47, n >= 100 && n <= 107:
				return true
			}
		}
	}
	return false
}

// TestBoardRows_NoBackground: no board row paints a background, selected or
// not, in any view or theme. The selection is the arrow and the name only.
func TestBoardRows_NoBackground(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for _, name := range theme.Names() {
		applyTheme(name)
		withProfile(termenv.TrueColor, func() {
			for _, icons := range []bool{true, false} {
				for _, detailed := range []bool{false, true} {
					for _, w := range []int{50, 80, 120, 150} {
						m := goldenIcons(t, goldenSize{w, 40}, detailed, "loops", true, "row_icons", icons)
						for _, s := range m.Sessions {
							m.SelectedID = s.ID
							board := renderBoard(m, w, 0)
							require.False(t, paintsBackground(board),
								"%s icons=%v detailed=%v width %d selected %s: %q", name, icons, detailed, w, s.ID, board)
						}
					}
				}
			}
		})
	}
}

// TestBoardRows_SelectionStillShows: without a bar, the selected row keeps
// its arrow, and a marked-done row's name does not recede while selected.
func TestBoardRows_SelectionStillShows(t *testing.T) {
	m := goldenIcons(t, goldenSize{120, 40}, false)
	m.SelectedID = "i3"
	board := ansi.Strip(renderBoard(m, 120, 0))
	line := ""
	for _, l := range strings.Split(board, "\n") {
		if strings.Contains(l, "invoice-rounding") {
			line = l
		}
	}
	require.True(t, strings.HasPrefix(line, " "+selectGlyph()+" "), "the arrow marks the selection: %q", line)

	done := protocol.SessionSummary{RetireAt: goldenNow}
	require.Equal(t, styleSlug, rowNameStyle(done, true), "selected: the name reads at full weight")
	require.Equal(t, styleDim, rowNameStyle(done, false))
}

// TestIconStrip_RightAnchored: the strip ends one gap before the age at every
// class and view; the detail columns sit before it, after the name.
func TestIconStrip_RightAnchored(t *testing.T) {
	for _, w := range []int{80, 120, 150, 200} {
		for _, detailed := range []bool{false, true} {
			m := iconLayoutModel(t, iconNow, w, iconSessions(iconNow), "usage", true)
			m.Detailed = detailed
			c := boardLayout(m, w)
			s := m.Sessions[2] // subagents, spend, long run
			row := ansi.Strip(renderRow(m, s, c))
			end := w - colTime - colGap
			lw := slotWidth[slotLongRun]
			require.Equal(t, ansi.Strip(longRunCell(m.now(), s, lw)), ansi.Cut(row, end-lw, end),
				"width %d detailed=%v: long-run is the strip's last slot, next to the age: %q", w, detailed, row)
			require.Equal(t, strings.Repeat(" ", colGap), ansi.Cut(row, end, end+colGap))
			if detailed && c.model > 0 {
				mi := strings.Index(row, "opus · max")
				require.Positive(t, mi)
				require.Less(t, mi, strings.Index(row, "⋔ 12"), "width %d: detail columns before the strip", w)
			}
		}
	}
}

// TestIconStrip_NameTakesFreedWidth: the blank room left of the strip
// belongs to the name, so a long name is not cut while there is room.
func TestIconStrip_NameTakesFreedWidth(t *testing.T) {
	long := "payment-migration-v2-with-a-very-long-name" // 42 cells, over every class width
	for _, w := range []int{120, 150} {
		m := iconLayoutModel(t, iconNow, w, iconSessions(iconNow))
		m.Detailed = false
		c := boardLayout(m, w)
		require.Greater(t, c.slug+c.spacer, len(long), "width %d", w)
		require.Contains(t, ansi.Strip(renderRow(m, m.Sessions[2], c)), long, "width %d", w)
	}
	m := iconLayoutModel(t, iconNow, 80, iconSessions(iconNow))
	m.Detailed = false
	c := boardLayout(m, 80)
	row := ansi.Strip(renderRow(m, m.Sessions[2], c))
	require.Contains(t, row, "…", "narrow: still cut, to its column")
	require.Equal(t, 80, ansi.StringWidth(row))
}
