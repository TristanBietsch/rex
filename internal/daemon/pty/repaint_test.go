package pty

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
	"github.com/stretchr/testify/require"
)

// reproducedModes are the mode bits Repaint promises to re-create.
const reproducedModes = vt10x.ModeWrap | vt10x.ModeInsert | vt10x.ModeAppKeypad | vt10x.ModeAltScreen |
	vt10x.ModeCRLF | vt10x.ModeMouseMask | vt10x.ModeReverse | vt10x.ModeHide | vt10x.ModeAppCursor |
	vt10x.ModeMouseSgr | vt10x.ModeFocus

func newScreen(t *testing.T, cols, rows int, feed ...string) vt10x.Terminal {
	t.Helper()
	term := vt10x.New(vt10x.WithSize(cols, rows))
	for _, s := range feed {
		_, err := term.Write([]byte(s))
		require.NoError(t, err)
	}
	return term
}

// repaintOf serializes src and feeds the result to a fresh terminal of the
// same size, as an attach client's emulator is.
func repaintOf(t *testing.T, src vt10x.Terminal) (vt10x.Terminal, []byte) {
	t.Helper()
	src.Lock()
	b := Repaint(src)
	cols, rows := src.Size()
	src.Unlock()
	return newScreen(t, cols, rows, string(b)), b
}

type cellView struct {
	Char   rune
	Mode   int16
	FG, BG vt10x.Color
}

func viewOf(g vt10x.Glyph) cellView {
	return cellView{Char: g.Char, Mode: g.Mode &^ vt10x.AttrWrap, FG: g.FG, BG: g.BG}
}

type cursorView struct {
	X, Y    int
	State   uint8
	Mode    int16
	FG, BG  vt10x.Color
	Visible bool
}

func cursorOf(c vt10x.Cursor, cols, rows int) cursorView {
	return cursorView{X: min(c.X, cols-1), Y: min(c.Y, rows-1), State: c.State,
		Mode: c.Attr.Mode & penModes, FG: c.Attr.FG, BG: c.Attr.BG}
}

// requireSameScreen compares everything Repaint reproduces: the screen cell
// by cell (the wrap mark aside), the primary screen under an alternate one,
// the cursor and its pen, the saved cursor, the modes and the scroll region.
//
// The alternate buffer behind the primary screen is not compared: it is
// blank unless vt10x quirks left old cells there (RIS or ?1049l on the
// primary screen swap buffers without clearing), and real terminals clear
// it on entry anyway.
func requireSameScreen(t *testing.T, want, got vt10x.Terminal, msgAndArgs ...any) {
	t.Helper()
	want.Lock()
	defer want.Unlock()
	got.Lock()
	defer got.Unlock()
	cols, rows := want.Size()
	gc, gr := got.Size()
	require.Equal(t, [2]int{cols, rows}, [2]int{gc, gr}, msgAndArgs...)
	type buffer struct {
		name string
		cell func(v vt10x.View, x, y int) vt10x.Glyph
	}
	buffers := []buffer{{"active", vt10x.View.Cell}}
	if want.Mode()&vt10x.ModeAltScreen != 0 {
		buffers = append(buffers, buffer{"primary under alt", vt10x.View.InactiveCell})
	}
	for _, buf := range buffers {
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				w, g := viewOf(buf.cell(want, x, y)), viewOf(buf.cell(got, x, y))
				if w != g {
					require.Failf(t, "cell differs", "%s screen (%d,%d): want %+v got %+v\nwant:\n%s\ngot:\n%s\n%s",
						buf.name, x, y, w, g, dump(want), dump(got), fmt.Sprint(msgAndArgs...))
				}
			}
		}
	}
	wc, gcur := cursorOf(want.Cursor(), cols, rows), cursorOf(got.Cursor(), cols, rows)
	wc.Visible, gcur.Visible = want.CursorVisible(), got.CursorVisible()
	require.Equal(t, wc, gcur, append([]any{"cursor"}, msgAndArgs...)...)
	ws, gs := cursorOf(want.SavedCursor(), cols, rows), cursorOf(got.SavedCursor(), cols, rows)
	ws.State &^= vt10x.CursorWrapNext // restoring always clears it
	gs.State &^= vt10x.CursorWrapNext
	require.Equal(t, ws, gs, append([]any{"saved cursor"}, msgAndArgs...)...)
	require.Equal(t, want.Mode()&reproducedModes, got.Mode()&reproducedModes, append([]any{"modes"}, msgAndArgs...)...)
	wt, wb := want.ScrollRegion()
	gt, gb := got.ScrollRegion()
	require.Equal(t, [2]int{wt, wb}, [2]int{gt, gb}, append([]any{"scroll region"}, msgAndArgs...)...)
}

func dump(v vt10x.View) string {
	cols, rows := v.Size()
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			c := v.Cell(x, y).Char
			if c == 0 {
				c = '.'
			}
			sb.WriteRune(c)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// probes are written to both terminals after a round trip: output the agent
// sends next must land the same way (pen, pending wrap, saved cursor,
// scroll region, origin mode, the primary screen under the alternate one).
var probes = []string{
	"Y",
	"\x1b8Z",
	"\r\n\n\nW\x1bMV",
	leaveAlt,
	"\x1b[2;1Hq\x1b[0mq",
}

// leaveAlt is only probed on the alternate screen: on the primary one vt10x
// swaps in the unrepainted alternate buffer (see requireSameScreen).
const leaveAlt = "\x1b[?1049l"

func onAlt(v vt10x.Terminal) bool {
	v.Lock()
	defer v.Unlock()
	return v.Mode()&vt10x.ModeAltScreen != 0
}

func requireRoundTrip(t *testing.T, src vt10x.Terminal, label string) {
	t.Helper()
	dst, b := repaintOf(t, src)
	requireSameScreen(t, src, dst, label, fmt.Sprintf("repaint %q", b))
	for _, p := range probes {
		if p == leaveAlt && !onAlt(src) {
			continue
		}
		for _, term := range []vt10x.Terminal{src, dst} {
			_, err := term.Write([]byte(p))
			require.NoError(t, err)
		}
		requireSameScreen(t, src, dst, label, fmt.Sprintf("after probe %q", p))
	}
}

func TestRepaint_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		cols int
		rows int
		feed []string
	}{
		{"blank", 20, 5, nil},
		{"attributes and colors", 60, 6, []string{
			"\x1b[1;31mbold red\x1b[0m \x1b[3;4mitalic-ul\x1b[0m \x1b[5mblink\x1b[25m \x1b[7mrev\x1b[27m\r\n",
			"\x1b[38;5;208m256\x1b[48;2;10;20;30mrgb\x1b[0m \x1b[1;38;5;3mbold3\x1b[0m \x1b[7;1;32;44mrevbold\x1b[0m\r\n",
			"\x1b[92;104mbright\x1b[0m \x1b[38;2;0;0;5mtiny-rgb\x1b[0m \x1b[39;49mdefault",
		}},
		{"erase under a pen", 30, 6, []string{
			"text everywhere\r\nmore text here",
			"\x1b[1;33m\x1b[2;3H\x1b[K\x1b[0m\x1b[7m\x1b[1;5H\x1b[X\x1b[0m\x1b[44m\x1b[4;1H\x1b[2K",
			"\x1b[1;7;31m\x1b[5;2H\x1b[3X\x1b[0m",
		}},
		{"reverse clear", 12, 4, []string{"\x1b[7m\x1b[2J\x1b[0m\x1b[2;2Hx"}},
		{"line drawing", 20, 3, []string{"\x1b(0lqqk\x1b(B text \x1b(0x\x1b(B\r\n\x1b(0mqj"}},
		{"wide runes", 20, 3, []string{"日本語 ✓ 🎉 é\r\n\x1b[1;5H中"}},
		{"pending wrap", 10, 3, []string{"\x1b[2;1H0123456789"}},
		{"pending wrap on a reversed cell", 10, 3, []string{"\x1b[2;1H012345678\x1b[7;35mX"}},
		{"hidden cursor", 10, 3, []string{"abc\x1b[?25l"}},
		{"scroll region and origin mode", 20, 10, []string{"\x1b[3;8r\x1b[?6h\x1b[2;3Hin-region"}},
		{"scroll region scrolled", 20, 8, []string{"top\x1b[8;1Hbottom\x1b[2;6r\x1b[6;1Ha\nb\nc\nd"}},
		{"saved cursor", 20, 8, []string{"\x1b[5;7H\x1b[1;35m\x1b(0\x1b7\x1b(B\x1b[0m\x1b[H"}},
		{"saved cursor in origin mode", 20, 8, []string{"\x1b[2;7r\x1b[?6h\x1b[3;4H\x1b7\x1b[?6l\x1b[r"}},
		{"alt screen over primary", 24, 5, []string{
			"\x1b[32mprimary line\x1b[0m\r\nsecond\x1b[3;3H",
			"\x1b[?1049h\x1b[H\x1b[2J\x1b[1malt content\x1b[0m\x1b[4;6H",
		}},
		{"alt screen saved cursor moved", 24, 5, []string{
			"p\x1b[2;2H\x1b[?1049hA\x1b[4;4H\x1b[4m\x1b7\x1b[0m\x1b[H",
		}},
		{"modes", 20, 4, []string{"\x1b[?1h\x1b=\x1b[?1002h\x1b[?1006h\x1b[?1004h\x1b[4h\x1b[20h\x1b[?5h\x1b[?7l"}},
		{"x10 mouse", 20, 4, []string{"\x1b[?9h"}},
		{"any-motion mouse", 20, 4, []string{"\x1b[?1003h\x1b[?1000h\x1b[?1003h"}},
		{"pen left set", 20, 4, []string{"\x1b[1;4;38;2;1;2;3;48;5;100mtext"}},
		{"line drawing left on", 20, 4, []string{"\x1b(0q"}},
		{"full screen", 8, 4, []string{"11111111\r\n22222222\r\n33333333\r\n44444444"}},
		{"scrolled", 10, 3, []string{"1\r\n2\r\n3\r\n4\r\n5\r\n6"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireRoundTrip(t, newScreen(t, tc.cols, tc.rows, tc.feed...), tc.name)
		})
	}
}

// A screen resized after its cursor was saved keeps the saved position
// unclamped; the repaint restores to the same place.
func TestRepaint_SavedCursorBeyondShrunkScreen(t *testing.T) {
	src := newScreen(t, 40, 20, "x\x1b[18;35H\x1b7\x1b[H")
	src.Resize(20, 10)
	requireRoundTrip(t, src, "shrunk")
}

// TestRepaint_NeverScrolls: a repaint fed to a terminal with scrollback
// capture (the attach popup's) never hands it a line.
func TestRepaint_NeverScrolls(t *testing.T) {
	for _, feed := range []string{
		"1\r\n2\r\n3\r\n4\r\n5\r\n6",
		"abcdefghij\r\nabcdefghij\r\nabcdefghij",
		"\x1b[?1049hfull\x1b[3;10Hz",
	} {
		src := newScreen(t, 10, 3, feed)
		src.Lock()
		b := Repaint(src)
		src.Unlock()
		lost := 0
		dst := vt10x.New(vt10x.WithSize(10, 3), vt10x.WithScrollback(func([]vt10x.Glyph) { lost++ }))
		_, err := dst.Write(b)
		require.NoError(t, err)
		require.Zero(t, lost, "repaint of %q scrolled", feed)
		requireSameScreen(t, src, dst)
	}
}

// A repaint fed to a terminal holding something else (a real terminal after
// a transcript replay, or a smaller one) overwrites every visible row.
func TestRepaint_OverwritesDirtyTerminal(t *testing.T) {
	src := newScreen(t, 12, 4, "\x1b[2;3Hhello")
	src.Lock()
	b := Repaint(src)
	src.Unlock()
	dst := newScreen(t, 12, 4,
		"\x1b[41mgarbage!!!!!\r\ngarbage!!!!!\r\ngarbage!!!!!\r\ngarbage!!!!!",
		"\x1b[2;3r\x1b[?6h\x1b[4h\x1b[?7l\x1b[?1049h\x1b(0\x1b[7m")
	_, err := dst.Write(b)
	require.NoError(t, err)
	dst.Lock()
	text := dump(dst)
	dst.Unlock()
	require.Equal(t, "            \n  hello     \n            \n            \n", text)
}

func TestRepaint_EmptySize(t *testing.T) {
	require.Nil(t, Repaint(zeroView{vt10x.New(vt10x.WithSize(2, 2))}))
}

// zeroView reports a 0x0 size.
type zeroView struct{ vt10x.View }

func (zeroView) Size() (int, int) { return 0, 0 }

// TestRepaint_KeepsControlRunesOut: vt10x stores control characters as
// cells while the line-drawing charset is on; the repaint must not send
// them to a terminal.
func TestRepaint_KeepsControlRunesOut(t *testing.T) {
	src := newScreen(t, 10, 2, "\x1b(0a\x01b\u0085c\x1b(B")
	src.Lock()
	b := Repaint(src)
	src.Unlock()
	for _, r := range string(b) {
		if r == 0x1b {
			continue
		}
		require.False(t, r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0), "control rune %U in repaint %q", r, b)
	}
}

// repaintVocabulary is what TestRepaint_RandomScreens builds screens from:
// text, wide runes, cursor motion, erases, scrolls, insert/delete, SGR,
// charsets, modes, margins, save/restore and the alternate screen. Palette
// changes, tab stops and control characters printed as line-drawing cells
// are left out: Repaint does not reproduce those exactly.
var repaintVocabulary = []string{
	"a", "hello ", "日本", "🎉", "é", "─", "\r", "\n", "\r\n", "\b", "\x1bD", "\x1bM", "\x1bE",
	"\x1b[H", "\x1b[3;4H", "\x1b[99;99H", "\x1b[2A", "\x1b[3B", "\x1b[5C", "\x1b[2D", "\x1b[4G", "\x1b[2d",
	"\x1b[J", "\x1b[1J", "\x1b[2J", "\x1b[K", "\x1b[1K", "\x1b[2K", "\x1b[3X",
	"\x1b[S", "\x1b[2T", "\x1b[L", "\x1b[2M", "\x1b[2@", "\x1b[P",
	"\x1b[0m", "\x1b[1m", "\x1b[3m", "\x1b[4m", "\x1b[5m", "\x1b[7m", "\x1b[22m", "\x1b[27m",
	"\x1b[31m", "\x1b[44m", "\x1b[95m", "\x1b[102m", "\x1b[38;5;196m", "\x1b[48;5;22m",
	"\x1b[38;2;200;100;50m", "\x1b[48;2;1;2;3m", "\x1b[39m", "\x1b[49m",
	"\x1b(0", "\x1b(B", "lqk", "\x1b7", "\x1b8", "\x1b[s", "\x1b[u",
	"\x1b[2;5r", "\x1b[r", "\x1b[?6h", "\x1b[?6l", "\x1b[?7l", "\x1b[?7h", "\x1b[4h", "\x1b[4l",
	"\x1b[20h", "\x1b[20l", "\x1b[?25l", "\x1b[?25h", "\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?1047l",
	"\x1b[?1000h", "\x1b[?1003h", "\x1b[?1000l", "\x1b[?1006h", "\x1b[?1h", "\x1b=", "\x1b>", "\x1b[?5h", "\x1b[?1004h",
	"\x1bc", "0123456789abcdef",
}

// TestRepaint_RandomScreens round-trips screens built from random escape
// sequences (and resizes), with a fixed seed so failures reproduce.
func TestRepaint_RandomScreens(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // G404: test input, not security.
	for i := 0; i < 400; i++ {
		cols, rows := 4+rng.Intn(14), 2+rng.Intn(7)
		src := vt10x.New(vt10x.WithSize(cols, rows))
		var log strings.Builder
		for j := 0; j < 10+rng.Intn(60); j++ {
			if rng.Intn(40) == 0 {
				cols, rows = 4+rng.Intn(14), 2+rng.Intn(7)
				src.Resize(cols, rows)
				fmt.Fprintf(&log, "<resize %dx%d>", cols, rows)
				continue
			}
			s := repaintVocabulary[rng.Intn(len(repaintVocabulary))]
			log.WriteString(s)
			_, err := src.Write([]byte(s))
			require.NoError(t, err)
		}
		requireRoundTrip(t, src, fmt.Sprintf("case %d: %q", i, log.String()))
	}
}

// FuzzRepaint_RoundTrip is TestRepaint_RandomScreens with the fuzzer
// choosing the words.
func FuzzRepaint_RoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3}, uint8(10), uint8(4))
	f.Add([]byte{60, 1, 77, 2, 40, 5, 70}, uint8(8), uint8(3))
	f.Fuzz(func(t *testing.T, words []byte, cols, rows uint8) {
		c, r := 2+int(cols%30), 1+int(rows%12)
		src := vt10x.New(vt10x.WithSize(c, r))
		for _, w := range words {
			_, _ = src.Write([]byte(repaintVocabulary[int(w)%len(repaintVocabulary)]))
		}
		requireRoundTrip(t, src, fmt.Sprintf("%dx%d %v", c, r, words))
	})
}
