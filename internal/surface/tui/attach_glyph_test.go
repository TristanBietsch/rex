package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

// withTrueColor renders with colors on for the rest of the test.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// termRow writes out to a cols-wide emulator and renders row 0.
func termRow(t *testing.T, cols int, out string) (vt10x.Terminal, string) {
	t.Helper()
	term := vt10x.New(vt10x.WithSize(cols, 2))
	_, err := term.Write([]byte(out))
	require.NoError(t, err)
	term.Lock()
	defer term.Unlock()
	return term, attachGlyphRow(cols, func(x int) vt10x.Glyph { return term.Cell(x, 0) }, -1)
}

func TestGlyphStyle_SGRAttributes(t *testing.T) {
	withTrueColor(t)
	term, row := termRow(t, 12, "\x1b[7mR\x1b[0m\x1b[1mB\x1b[0m\x1b[4mU\x1b[0m\x1b[3mI\x1b[0m\x1b[31;7mC\x1b[0m")
	term.Lock()
	defer term.Unlock()
	rev := glyphStyle(term.Cell(0, 0))
	require.True(t, rev.GetReverse(), "SGR 7 on default colors")
	require.True(t, glyphStyle(term.Cell(1, 0)).GetBold())
	require.True(t, glyphStyle(term.Cell(2, 0)).GetUnderline())
	require.True(t, glyphStyle(term.Cell(3, 0)).GetItalic())
	red := glyphStyle(term.Cell(4, 0))
	require.True(t, red.GetReverse())
	require.Equal(t, lipgloss.TerminalColor(lipgloss.Color("1")), red.GetForeground(), "swap undone: red stays the FG")
	require.Equal(t, lipgloss.TerminalColor(lipgloss.NoColor{}), red.GetBackground())
	require.Regexp(t, `\x1b\[(\d+;)*7(;\d+)*mR`, row)
	require.Regexp(t, `\x1b\[(\d+;)*1(;\d+)*mB`, row)
	require.Regexp(t, `\x1b\[(\d+;)*4(;\d+)*mU`, row)
}

func TestAttachGlyphRow_WideRunesFitCols(t *testing.T) {
	cases := []struct {
		name, out, want string
	}{
		{"written in sequence", "你好世界abc", "你好世界abc"},
		{"placed by column", "你\x1b[1;3H好\x1b[1;5Hx", "你好x"},
		{"wider than the row", strings.Repeat("日", 20), strings.Repeat("日", 10)},
		{"straddles the edge", "a" + strings.Repeat("日", 10), "a" + strings.Repeat("日", 9)},
		{"emoji with selector", "❤️ ok", "❤️ ok"},
	}
	for _, c := range cases {
		_, row := termRow(t, 20, c.out)
		require.Equal(t, 20, ansi.StringWidth(row), c.name)
		require.Equal(t, c.want, strings.TrimRight(ansi.Strip(row), " "), c.name)
	}
}

func TestAttachGlyphRow_CursorAfterWideRuneStaysVisible(t *testing.T) {
	withTrueColor(t)
	term := vt10x.New(vt10x.WithSize(6, 1))
	_, _ = term.Write([]byte("你"))
	term.Lock()
	defer term.Unlock()
	cur := term.Cursor()
	require.Equal(t, 1, cur.X)
	row := attachGlyphRow(6, func(x int) vt10x.Glyph { return term.Cell(x, 0) }, cur.X)
	require.Contains(t, row, "\x1b[7m ", "cursor cell drawn")
	require.Equal(t, 6, ansi.StringWidth(row))
}

func TestAttachGlyphRow_CursorInvertsReversedCell(t *testing.T) {
	withTrueColor(t)
	term := vt10x.New(vt10x.WithSize(4, 1))
	_, _ = term.Write([]byte("\x1b[7mab\x1b[0mcd"))
	term.Lock()
	defer term.Unlock()
	row := attachGlyphRow(4, func(x int) vt10x.Glyph { return term.Cell(x, 0) }, 0)
	require.Equal(t, "a\x1b[7mb\x1b[0mcd", row, "cursor on a reversed cell shows it plain")
}
