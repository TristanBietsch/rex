package vt10x

import "testing"

// The exported attribute bits are what the rex attach popup draws from.
func TestExportedAttrBits(t *testing.T) {
	cases := []struct {
		sgr  string
		want int16
	}{
		{"\x1b[1m", AttrBold},
		{"\x1b[3m", AttrItalic},
		{"\x1b[4m", AttrUnderline},
		{"\x1b[7m", AttrReverse},
	}
	seen := int16(0)
	for _, tc := range cases {
		if tc.want == 0 || tc.want&(tc.want-1) != 0 || seen&tc.want != 0 {
			t.Fatalf("%q: bit %b is not a distinct single bit", tc.sgr, tc.want)
		}
		seen |= tc.want
		term := New(WithSize(2, 1))
		if _, err := term.Write([]byte(tc.sgr + "x")); err != nil {
			t.Fatal(err)
		}
		if got := term.Cell(0, 0).Mode; got != tc.want {
			t.Fatalf("%q: Mode = %b, want %b", tc.sgr, got, tc.want)
		}
	}
}

// A reversed cell is stored with its colors swapped; renderers rely on it.
func TestReverseStoresSwappedColors(t *testing.T) {
	term := New(WithSize(2, 1))
	if _, err := term.Write([]byte("\x1b[7;31;44mx")); err != nil {
		t.Fatal(err)
	}
	if g := term.Cell(0, 0); g.FG != Color(4) || g.BG != Color(1) || g.Mode&AttrReverse == 0 {
		t.Fatalf("cell = fg %v bg %v mode %b, want fg 4 bg 1 with AttrReverse", g.FG, g.BG, g.Mode)
	}
}
