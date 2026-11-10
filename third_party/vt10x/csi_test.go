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
// the pen (CSI > 4;2 m used to run SGR 0, CSI ? 4 m set underline), or the
// redraw that follows lands on the wrong rows.
func TestCSIMarkerSequencesAreIgnored(t *testing.T) {
	cases := []struct{ name, seq string }{
		{"kitty pop", "\x1b[<u"},
		{"kitty push", "\x1b[>5u"},
		{"kitty set", "\x1b[=1;1u"},
		{"kitty query", "\x1b[?u"},
		{"modifyOtherKeys", "\x1b[>4;2m"},
		{"XTQMODKEYS", "\x1b[?4m"},
		{"XTVERSION", "\x1b[>q"},
		{"DA2", "\x1b[>c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term := New(WithSize(40, 20))
			// Save the cursor at row 4 col 9, then move to row 11 col 2 in red.
			writeTerm(t, term, "\x1b[5;10H\x1b7\x1b[12;3H\x1b[31m")
			before := term.Cursor()
			writeTerm(t, term, tc.seq)
			after := term.Cursor()
			if after.X != before.X || after.Y != before.Y {
				t.Fatalf("%q moved the cursor from (%d,%d) to (%d,%d)", tc.seq, before.X, before.Y, after.X, after.Y)
			}
			if after.Attr.FG != before.Attr.FG || after.Attr.BG != before.Attr.BG || after.Attr.Mode != before.Attr.Mode {
				t.Fatalf("%q changed the pen: fg %v->%v bg %v->%v mode %d->%d", tc.seq,
					before.Attr.FG, after.Attr.FG, before.Attr.BG, after.Attr.BG, before.Attr.Mode, after.Attr.Mode)
			}
			writeTerm(t, term, "x")
			if g := term.Cell(2, 11); g.Char != 'x' || g.FG != Color(1) {
				t.Fatalf("cell (2,11) = %q fg %v, want red 'x'", g.Char, g.FG)
			}
		})
	}
}

// CSI ? Pm s is XTSAVE. It must not overwrite the position CSI s saved.
func TestCSIXTSaveDoesNotSaveCursor(t *testing.T) {
	term := New(WithSize(40, 20))
	writeTerm(t, term, "\x1b[3;4H\x1b[s\x1b[12;3H\x1b[?1s\x1b[1;1H\x1b[u")
	if cur := term.Cursor(); cur.X != 3 || cur.Y != 2 {
		t.Fatalf("CSI u restored to (%d,%d), want (3,2) from CSI s", cur.X, cur.Y)
	}
}

func writeTerm(t *testing.T, term Terminal, s string) {
	t.Helper()
	if _, err := term.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

func TestCSISaveRestoreCursorStillWorks(t *testing.T) {
	term := New(WithSize(40, 20))
	writeTerm(t, term, "\x1b[3;4H\x1b[s\x1b[10;10H\x1b[u")
	if cur := term.Cursor(); cur.X != 3 || cur.Y != 2 {
		t.Fatalf("CSI u restored to (%d,%d), want (3,2)", cur.X, cur.Y)
	}
}
