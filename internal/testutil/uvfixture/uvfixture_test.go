package uvfixture

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

func screenRows(term vt10x.Terminal) []string {
	term.Lock()
	defer term.Unlock()
	cols, rows := term.Size()
	out := make([]string, rows)
	for y := range out {
		r := make([]rune, cols)
		for x := range r {
			r[x] = term.Cell(x, y).Char
			if r[x] == 0 {
				r[x] = ' '
			}
		}
		out[y] = string(r)
	}
	return out
}

func TestLogoLinesShareOneWidth(t *testing.T) {
	for i, line := range Logo() {
		require.Equal(t, LogoWidth(), utf8.RuneCountInString(line), "line %d", i)
	}
}

// The frame has the sequences it claims to: REP, ECH, hard tabs and CUF,
// truecolor SGR, and no raw run of identical cells that REP replaces.
func TestFrameUsesTheSequencesUltravioletDoes(t *testing.T) {
	f := Frame(177, 44)
	for name, seq := range map[string][]byte{
		"REP":       []byte("\x1b[10b"),
		"REP rule":  []byte("=\x1b[20b"),
		"ECH":       []byte("\x1b[9X"),
		"CUF":       []byte("C"),
		"HT":        []byte("\t"),
		"truecolor": []byte("\x1b[38;2;"),
		"bg":        []byte("\x1b[48;2;"),
	} {
		require.True(t, bytes.Contains(f, seq), name)
	}
	require.False(t, bytes.Contains(f, []byte("==")), "the rule is one '=' plus REP")
}

// Against vt10x as it is now, the frame lays out as plain string math says,
// at widths below, at and above vt10x's original 120-column tab table.
func TestFrameLaysOutLikeStringMath(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 32}, {121, 32}, {177, 44}, {240, 60}} {
		cols, rows := size[0], size[1]
		term := vt10x.New(vt10x.WithSize(cols, rows))
		_, err := term.Write(Frame(cols, rows))
		require.NoError(t, err)
		require.Equal(t, Expected(cols, rows), screenRows(term), "%dx%d", cols, rows)
	}
}

func TestLayoutIsCentered(t *testing.T) {
	for cols := 60; cols < 250; cols++ {
		l := LayoutFor(cols, 40)
		right := cols - l.Left - l.LogoCols
		require.LessOrEqual(t, abs(l.Left-right), 1, "cols=%d", cols)
		require.Zero(t, l.TagCol%tabWidth)
		require.LessOrEqual(t, l.TagCol+len(Tag), cols)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestFrameRejectsTinyScreens(t *testing.T) {
	require.Panics(t, func() { Frame(30, 10) })
	require.Panics(t, func() { Frame(100, 5) })
}
