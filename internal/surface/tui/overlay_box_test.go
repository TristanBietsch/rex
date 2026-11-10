package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// overlayBox replaced lipgloss Border(RoundedBorder()).Padding(1, 2) for
// speed; it must draw the same box.
func TestOverlayBoxMatchesLipgloss(t *testing.T) {
	contents := []string{
		"one line",
		"short\na much longer second line\n",
		"\x1b[1mbold\x1b[0m and \x1b[38;5;174mcolor\x1b[0m\nplain",
		"wide 文字 chars\nx",
		"tab\tseparated",
		"crlf\r\nlines\r\n",
		"",
	}
	for _, profile := range []termenv.Profile{termenv.Ascii, termenv.ANSI256} {
		prev := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(profile)
		for _, c := range contents {
			want := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).Padding(1, 2).Render(c)
			lines, w := overlayBox(c)
			got := strings.Join(lines, "\n")
			if ansi.Strip(got) != ansi.Strip(want) {
				t.Errorf("profile %v content %q:\ngot\n%s\nwant\n%s", profile, c, ansi.Strip(got), ansi.Strip(want))
			}
			if profile == termenv.Ascii && got != want {
				t.Errorf("profile Ascii content %q: bytes differ", c)
			}
			for i, l := range lines {
				if lw := ansi.StringWidth(l); lw != w {
					t.Errorf("content %q line %d width %d, want %d", c, i, lw, w)
				}
			}
		}
		lipgloss.SetColorProfile(prev)
	}
}
