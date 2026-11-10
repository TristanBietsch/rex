package vt10x

import (
	"testing"
)

func TestCSIParse(t *testing.T) {
	var csi csiEscape
	csi.reset()
	csi.buf = []byte("s")
	csi.parse()
	if csi.mode != 's' || csi.arg(0, 17) != 17 || len(csi.args) != 0 {
		t.Fatal("CSI parse mismatch")
	}

	csi.reset()
	csi.buf = []byte("31T")
	csi.parse()
	if csi.mode != 'T' || csi.arg(0, 0) != 31 || len(csi.args) != 1 {
		t.Fatal("CSI parse mismatch")
	}

	csi.reset()
	csi.buf = []byte("48;2f")
	csi.parse()
	if csi.mode != 'f' || csi.arg(0, 0) != 48 || csi.arg(1, 0) != 2 || len(csi.args) != 2 {
		t.Fatal("CSI parse mismatch")
	}

	csi.reset()
	csi.buf = []byte("?25l")
	csi.parse()
	if csi.mode != 'l' || csi.arg(0, 0) != 25 || csi.priv != true || len(csi.args) != 1 {
		t.Fatal("CSI parse mismatch")
	}
}

func TestCSIParseMarkers(t *testing.T) {
	cases := []struct {
		buf    string
		mode   byte
		marker byte
		priv   bool
		args   []int
	}{
		{"<u", 'u', '<', false, nil},
		{">5u", 'u', '>', false, []int{5}},
		{">4;2m", 'm', '>', false, []int{4, 2}},
		{"=1;1u", 'u', '=', false, []int{1, 1}},
		{"?u", 'u', '?', true, nil},
		{"?25h", 'h', '?', true, []int{25}},
		{"u", 'u', 0, false, nil},
	}
	for _, tc := range cases {
		var csi csiEscape
		csi.reset()
		csi.buf = []byte(tc.buf)
		csi.parse()
		if csi.mode != tc.mode || csi.marker != tc.marker || csi.priv != tc.priv || len(csi.args) != len(tc.args) {
			t.Fatalf("parse %q: mode=%q marker=%q priv=%v args=%v", tc.buf, csi.mode, csi.marker, csi.priv, csi.args)
		}
		for i, a := range tc.args {
			if csi.args[i] != a {
				t.Fatalf("parse %q: args=%v, want %v", tc.buf, csi.args, tc.args)
			}
		}
	}
}

// Claude Code sends kitty keyboard and modifyOtherKeys sequences on every
// redraw. They must not move the cursor (they used to run DECRC) or touch
// the pen, or the redraw that follows lands on the wrong rows.
func TestCSIMarkerSequencesAreIgnored(t *testing.T) {
	term := New(WithSize(40, 20))
	// Save the cursor at row 4 col 9, then move to row 11 col 2 in red.
	if _, err := term.Write([]byte("\x1b[5;10H\x1b7\x1b[12;3H\x1b[31m")); err != nil {
		t.Fatal(err)
	}
	seqs := []string{
		"\x1b[<u",    // kitty: pop keyboard flags
		"\x1b[>5u",   // kitty: push keyboard flags
		"\x1b[=1;1u", // kitty: set keyboard flags
		"\x1b[?u",    // kitty: query keyboard flags
		"\x1b[>4;2m", // xterm modifyOtherKeys
		"\x1b[?4m",   // XTQMODKEYS query
		"\x1b[?1s",   // XTSAVE
		"\x1b[>q",    // XTVERSION query
		"\x1b[>c",    // DA2 query
	}
	for _, s := range seqs {
		if _, err := term.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
		cur := term.Cursor()
		if cur.X != 2 || cur.Y != 11 {
			t.Fatalf("after %q cursor at (%d,%d), want (2,11)", s, cur.X, cur.Y)
		}
		if cur.Attr.FG != Color(1) {
			t.Fatalf("after %q FG = %v, want red", s, cur.Attr.FG)
		}
	}
	if _, err := term.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if g := term.Cell(2, 11); g.Char != 'x' || g.FG != Color(1) {
		t.Fatalf("cell (2,11) = %q fg %v, want red 'x'", g.Char, g.FG)
	}
}

func TestCSISaveRestoreCursorStillWorks(t *testing.T) {
	term := New(WithSize(40, 20))
	if _, err := term.Write([]byte("\x1b[3;4H\x1b[s\x1b[10;10H\x1b[u")); err != nil {
		t.Fatal(err)
	}
	if cur := term.Cursor(); cur.X != 3 || cur.Y != 2 {
		t.Fatalf("CSI u restored to (%d,%d), want (3,2)", cur.X, cur.Y)
	}
}
