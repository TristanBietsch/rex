package vt10x

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func lineText(l []Glyph) string {
	var b strings.Builder
	for _, g := range l {
		b.WriteRune(g.Char)
	}
	return b.String()
}

func newScrollbackTerm(cols, rows int) (Terminal, *[]string) {
	var got []string
	term := New(WithSize(cols, rows), WithScrollback(func(l []Glyph) {
		got = append(got, lineText(l))
	}))
	return term, &got
}

func TestScrollbackCapturesLinesScrolledOffTop(t *testing.T) {
	term, got := newScrollbackTerm(20, 5)
	for i := 0; i < 12; i++ {
		fmt.Fprintf(term, "line %d\r\n", i)
	}
	// 12 lines + the empty cursor line on a 5-row screen: 8 scrolled off.
	want := []string{"line 0", "line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7"}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %q, want %q", *got, want)
	}
}

func TestScrollbackTrimsTrailingDefaultBlanksOnly(t *testing.T) {
	var got [][]Glyph
	term := New(WithSize(10, 2), WithScrollback(func(l []Glyph) { got = append(got, l) }))
	// Colored background trailing cells survive; default blanks are trimmed.
	fmt.Fprint(term, "ab\x1b[41m  \x1b[0m\r\n\r\n\r\n")
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2", len(got))
	}
	if len(got[0]) != 4 || got[0][3].BG != Red {
		t.Fatalf("first line = %+v, want 4 glyphs ending in red bg", got[0])
	}
	if len(got[1]) != 0 {
		t.Fatalf("blank line should trim to empty, got %d glyphs", len(got[1]))
	}
}

func TestScrollbackPassesCopy(t *testing.T) {
	var got [][]Glyph
	term := New(WithSize(10, 2), WithScrollback(func(l []Glyph) { got = append(got, l) }))
	fmt.Fprint(term, "keep\r\n\r\nxxxxxxxxxx\r\nyyyyyyyyyy")
	if lineText(got[0]) != "keep" {
		t.Fatalf("captured line was mutated: %q", lineText(got[0]))
	}
}

func TestScrollbackSkipsAltScreen(t *testing.T) {
	term, got := newScrollbackTerm(20, 3)
	fmt.Fprint(term, "\x1b[?1049h")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(term, "alt %d\r\n", i)
	}
	fmt.Fprint(term, "\x1b[?1049l")
	if len(*got) != 0 {
		t.Fatalf("alt screen leaked into scrollback: %q", *got)
	}
}

func TestScrollbackScrollRegions(t *testing.T) {
	// Region with top > 0 (a pinned header): nothing leaves the screen.
	term, got := newScrollbackTerm(20, 6)
	fmt.Fprint(term, "\x1b[3;6r\x1b[6;1H")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(term, "r%d\r\n", i)
	}
	if len(*got) != 0 {
		t.Fatalf("scroll region with top>0 captured %q", *got)
	}

	// Region anchored at the top with a pinned footer (inline TUIs insert
	// history this way): lines leaving the top are history.
	term, got = newScrollbackTerm(20, 6)
	fmt.Fprint(term, "\x1b[1;4r\x1b[1;1H")
	for i := 0; i < 6; i++ {
		fmt.Fprintf(term, "h%d\r\n", i)
	}
	if want := []string{"h0", "h1", "h2"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %q, want %q", *got, want)
	}
}

func TestScrollbackScrollUpAndIndex(t *testing.T) {
	term, got := newScrollbackTerm(20, 4)
	fmt.Fprint(term, "a\r\nb\r\nc\r\nd\x1b[2S")
	if want := []string{"a", "b"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("SU: got %q, want %q", *got, want)
	}
	// IND at the bottom row scrolls one line out.
	fmt.Fprint(term, "\x1b[4;1H\x1bD")
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("IND: got %q, want %q", *got, want)
	}
}

func TestScrollbackIgnoresDeleteLines(t *testing.T) {
	term, got := newScrollbackTerm(20, 4)
	fmt.Fprint(term, "a\r\nb\x1b[1;1H\x1b[M")
	if len(*got) != 0 {
		t.Fatalf("DL is an edit, not a scroll: captured %q", *got)
	}
}

func TestScrollbackCapturesRowsDroppedByResize(t *testing.T) {
	term, got := newScrollbackTerm(20, 6)
	fmt.Fprint(term, "r0\r\nr1\r\nr2\r\nr3\r\nr4\r\nr5")
	term.Resize(20, 3) // cursor on row 5 → rows 0..2 slide off the top
	if want := []string{"r0", "r1", "r2"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %q, want %q", *got, want)
	}
	if s := term.String(); !strings.HasPrefix(s, "r3") {
		t.Fatalf("screen after resize = %q", s)
	}
}

func TestNoScrollbackHookIsNoop(t *testing.T) {
	term := New(WithSize(10, 2))
	fmt.Fprint(term, "a\r\nb\r\nc\r\n")
	term.Resize(10, 1)
}
