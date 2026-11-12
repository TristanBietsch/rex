package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
)

func paintedRow(t *testing.T, p *glyphPainter, input string, cols, cursorX int) string {
	t.Helper()
	term := vt10x.New(vt10x.WithSize(cols, 1))
	if _, err := term.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	term.Lock()
	defer term.Unlock()
	cells := make([]vt10x.Glyph, cols)
	for x := range cells {
		cells[x] = term.Cell(x, 0)
	}
	return p.row(cells, cursorX)
}

func TestGlyphPainterSGR(t *testing.T) {
	p := newGlyphPainter(termenv.TrueColor)
	cases := []struct {
		name, input string
		cols        int
		want        string
	}{
		{"plain", "ab", 2, "ab"},
		{"bold", "\x1b[1mab", 2, "\x1b[1mab\x1b[0m"},
		{"italic underline", "\x1b[3;4mab", 2, "\x1b[3;4mab\x1b[0m"},
		{"reverse", "\x1b[7mab", 2, "\x1b[7mab\x1b[0m"},
		// vt10x stores these with FG and BG swapped; drawn once, not twice.
		{"reverse with fg", "\x1b[7;31mab", 2, "\x1b[7;31mab\x1b[0m"},
		{"reverse with fg and bg", "\x1b[7;31;44mab", 2, "\x1b[7;31;44mab\x1b[0m"},
		{"ansi fg", "\x1b[31mab", 2, "\x1b[31mab\x1b[0m"},
		{"256 fg and bg", "\x1b[38;5;174;48;5;16mab", 2, "\x1b[38;5;174;48;5;16mab\x1b[0m"},
		{"truecolor", "\x1b[38;2;217;119;87mab", 2, "\x1b[38;2;217;119;87mab\x1b[0m"},
		{"runs", "a\x1b[31mb\x1b[0mc", 3, "a\x1b[31mb\x1b[0mc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paintedRow(t, p, tc.input, tc.cols, -1); got != tc.want {
				t.Fatalf("row(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestGlyphPainterCursorFlipsReverse(t *testing.T) {
	p := newGlyphPainter(termenv.ANSI256)
	if got, want := paintedRow(t, p, "ab", 2, 1), "a\x1b[7mb\x1b[0m"; got != want {
		t.Fatalf("cursor on plain cell = %q, want %q", got, want)
	}
	// An agent's own reversed "cursor" cell under our cursor turns back.
	if got, want := paintedRow(t, p, "\x1b[7mab", 2, 0), "a\x1b[7mb\x1b[0m"; got != want {
		t.Fatalf("cursor on reversed cell = %q, want %q", got, want)
	}
	if got, want := paintedRow(t, p, "\x1b[7;31mab", 2, 0), "\x1b[31ma\x1b[0m\x1b[7;31mb\x1b[0m"; got != want {
		t.Fatalf("cursor on reversed red cell = %q, want %q", got, want)
	}
}

func TestGlyphPainterAsciiDrawsPlainText(t *testing.T) {
	p := newGlyphPainter(termenv.Ascii)
	if got := paintedRow(t, p, "\x1b[1;7;38;5;174mab", 2, 0); got != "ab" {
		t.Fatalf("Ascii row = %q, want plain %q", got, "ab")
	}
}

func TestLiveRowCache(t *testing.T) {
	st := &AttachState{SessionID: "a", Cols: 6, Rows: 2}
	st.Term = newAttachTerm(st, 6, 2, 0)
	st.paint = newGlyphPainter(termenv.ANSI256)
	write := func(s string) {
		t.Helper()
		if _, err := st.Term.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	row := func(y, cursorX int) string {
		st.Term.Lock()
		defer st.Term.Unlock()
		return st.liveRow(y, 6, cursorX)
	}
	write("hello")
	first := row(0, -1)
	if ansi.Strip(first) != "hello " {
		t.Fatalf("row 0 = %q", first)
	}
	if again := row(0, -1); again != first {
		t.Fatalf("unchanged row re-rendered differently: %q vs %q", again, first)
	}
	write("\x1b[1;5H\x1b[31mO")
	if got := ansi.Strip(row(0, -1)); got != "hellO " {
		t.Fatalf("after a cell change row 0 = %q, want %q", got, "hellO ")
	}
	if got := row(0, 0); !strings.HasPrefix(got, "\x1b[7mh") {
		t.Fatalf("cursor move not drawn: %q", got)
	}
	if got := ansi.Strip(row(1, -1)); got != "      " {
		t.Fatalf("row 1 = %q", got)
	}
}

// vt10x gives every rune one cell, but a terminal draws CJK and emoji over
// two columns: a painted row must still be exactly cols columns wide, or the
// popup re-centers and mouse hit-testing lands on the wrong cells.
func TestGlyphPainterWideRunesFitCols(t *testing.T) {
	p := newGlyphPainter(termenv.TrueColor)
	cases := []struct{ name, input, want string }{
		{"written in sequence", "你好世界abc", "你好世界abc"},
		{"placed by column", "你\x1b[1;3H好\x1b[1;5Hx", "你好x"},
		{"wider than the row", strings.Repeat("日", 20), strings.Repeat("日", 10)},
		{"straddles the edge", "a" + strings.Repeat("日", 10), "a" + strings.Repeat("日", 9)},
		{"emoji with selector", "❤️ ok", "❤️ ok"},
		{"narrow only", "abc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := paintedRow(t, p, tc.input, 20, -1)
			if w := ansi.StringWidth(row); w != 20 {
				t.Fatalf("row(%q) is %d columns, want 20: %q", tc.input, w, row)
			}
			if got := strings.TrimRight(ansi.Strip(row), " "); got != tc.want {
				t.Fatalf("row(%q) shows %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// The cursor cell right after a wide rune is blank but is never taken as
// the rune's right half: the cursor stays visible.
func TestGlyphPainterCursorAfterWideRune(t *testing.T) {
	p := newGlyphPainter(termenv.TrueColor)
	row := paintedRow(t, p, "你", 6, 1)
	if !strings.Contains(row, "\x1b[7m ") {
		t.Fatalf("cursor cell not drawn: %q", row)
	}
	if w := ansi.StringWidth(row); w != 6 {
		t.Fatalf("row is %d columns, want 6: %q", w, row)
	}
}
