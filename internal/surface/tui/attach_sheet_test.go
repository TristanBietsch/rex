package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// attachAt is an attached model on a w x h terminal with the banner mode and
// mobile_layout given, sized through resizeAttach like a real resize.
func attachAt(t *testing.T, w, h int, banner, mobile string) Model {
	t.Helper()
	m := bannerModel(t, w, h, bannerSession())
	require.NoError(t, m.Store.Set("attach_banner", banner))
	require.NoError(t, m.Store.Set("mobile_layout", mobile))
	resizeAttach(m)
	return m
}

// The agent's first cell shows a marker at the origin the mouse maps from, so
// the origin cannot drift from what is drawn.
func assertOriginMatchesRender(t *testing.T, m Model, label string) {
	t.Helper()
	_, _ = m.Attach.Term.Write([]byte("Q"))
	ox, oy := attachInnerOrigin(m)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	require.Greater(t, len(lines), oy, label)
	require.Equal(t, "Q", ansi.Cut(lines[oy], ox, ox+1), "%s: origin (%d,%d)", label, ox, oy)
}

func TestAttachSheet_FillsTheScreen(t *testing.T) {
	for _, w := range []int{30, 40, 45, 60} {
		for _, h := range []int{24, 30} {
			for _, banner := range []string{"static", "off"} {
				label := fmt.Sprintf("%dx%d banner=%s", w, h, banner)
				m := attachAt(t, w, h, banner, "auto")
				c := attachChromeOf(m)
				require.True(t, c.sheet, label)
				require.Equal(t, w-2, m.Attach.Cols, label)

				lines := strings.Split(ansi.Strip(m.View()), "\n")
				require.Len(t, lines, h, label)
				for i, l := range lines {
					require.Equal(t, w, ansi.StringWidth(l), "%s line %d", label, i)
				}
				require.True(t, strings.HasPrefix(lines[0], "╭"), "%s: the sheet starts at the top: %q", label, lines[0])
				require.True(t, strings.HasPrefix(lines[h-1], "╰"), "%s: and ends at the bottom", label)
				assertOriginMatchesRender(t, m, label)
			}
		}
	}
}

func TestAttachSheet_OnlyInMobileLayout(t *testing.T) {
	require.False(t, attachChromeOf(attachAt(t, 61, 30, "static", "auto")).sheet)
	require.True(t, attachChromeOf(attachAt(t, 61, 30, "static", "always")).sheet)
	require.False(t, attachChromeOf(attachAt(t, 40, 30, "static", "off")).sheet)
	require.True(t, attachChromeOf(attachAt(t, 60, 30, "static", "auto")).sheet)
}

// With the sheet off the popup is still never wider than the screen, and its
// origin matches the render.
func TestAttachPopup_ClampedToTheScreen(t *testing.T) {
	for w := 30; w <= 70; w++ {
		for _, banner := range []string{"static", "off"} {
			label := fmt.Sprintf("w=%d banner=%s", w, banner)
			m := attachAt(t, w, 30, banner, "off")
			boxW, _ := attachChromeOf(m).boxSize(m.Attach.Cols, m.Attach.Rows)
			require.LessOrEqual(t, boxW, w, label)
			for i, l := range strings.Split(m.View(), "\n") {
				require.LessOrEqual(t, ansi.StringWidth(l), w, "%s line %d", label, i)
			}
			assertOriginMatchesRender(t, m, label)
		}
	}
}

func TestAttachChrome_DesktopSizeUnchanged(t *testing.T) {
	cols, rows := attachChrome{}.innerSize(120, 40)
	require.Equal(t, 106, cols)
	require.Equal(t, 28, rows)
	cols, rows = attachChrome{banner: 1}.innerSize(120, 40)
	require.Equal(t, 106, cols)
	require.Equal(t, 27, rows)
	cols, _ = attachChrome{}.innerSize(50, 40)
	require.Equal(t, 40, cols, "the 40-column floor holds while the popup fits")
	cols, _ = attachChrome{}.innerSize(44, 40)
	require.Equal(t, 38, cols, "below that the popup follows the screen: 44-6")
}

// Crossing the breakpoint while attached re-sizes the agent between popup and
// sheet.
func TestAttachResizeAcrossSheetBreakpoint(t *testing.T) {
	m := attachAt(t, 80, 30, "static", "auto")
	require.Equal(t, 66, m.Attach.Cols)
	m.Width = 50
	resizeAttach(m)
	require.Equal(t, 48, m.Attach.Cols)
	assertOriginMatchesRender(t, m, "sheet after resize")
	m.Width = 80
	resizeAttach(m)
	require.Equal(t, 66, m.Attach.Cols)
	assertOriginMatchesRender(t, m, "popup after resize")
}
