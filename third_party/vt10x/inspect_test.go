package vt10x

import "testing"

// The accessors below let rex serialize a screen (its daemon re-creates a
// session's screen for a client that attaches late).

func write(t *testing.T, term Terminal, s string) {
	t.Helper()
	if _, err := term.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

func TestInactiveCellIsThePrimaryUnderTheAltScreen(t *testing.T) {
	term := New(WithSize(4, 2))
	write(t, term, "\x1b[31mab")
	if g := term.InactiveCell(0, 0); g.Char != ' ' {
		t.Fatalf("alt buffer before use = %q, want blank", g.Char)
	}
	write(t, term, "\x1b[?1049h\x1b[Hz")
	if g := term.Cell(0, 0); g.Char != 'z' {
		t.Fatalf("active cell = %q, want z", g.Char)
	}
	if g := term.InactiveCell(1, 0); g.Char != 'b' || g.FG != Red {
		t.Fatalf("inactive cell = %q fg %v, want b in red", g.Char, g.FG)
	}
	write(t, term, "\x1b[?1049l")
	if g := term.InactiveCell(0, 0); g.Char != ' ' {
		t.Fatalf("alt buffer after leaving = %q, want blank (cleared on exit)", g.Char)
	}
}

func TestSavedCursor(t *testing.T) {
	term := New(WithSize(10, 5))
	write(t, term, "\x1b[3;4H\x1b[1;32m\x1b7\x1b[H\x1b[0m")
	s := term.SavedCursor()
	if s.X != 3 || s.Y != 2 || s.Attr.Mode&AttrBold == 0 || s.Attr.FG != Green {
		t.Fatalf("saved = (%d,%d) mode %b fg %v, want (3,2) bold green", s.X, s.Y, s.Attr.Mode, s.Attr.FG)
	}
	write(t, term, "\x1b[2;2H\x1b[?1049h")
	if s := term.SavedCursor(); s.X != 1 || s.Y != 1 {
		t.Fatalf("1049 saved (%d,%d), want (1,1)", s.X, s.Y)
	}
}

func TestScrollRegion(t *testing.T) {
	term := New(WithSize(10, 6))
	if top, bottom := term.ScrollRegion(); top != 0 || bottom != 5 {
		t.Fatalf("default region %d..%d, want 0..5", top, bottom)
	}
	write(t, term, "\x1b[2;4r")
	if top, bottom := term.ScrollRegion(); top != 1 || bottom != 3 {
		t.Fatalf("region %d..%d, want 1..3", top, bottom)
	}
}

func TestExportedStateBits(t *testing.T) {
	term := New(WithSize(3, 3))
	write(t, term, "\x1b[5m\x1b(0qx\x1b(B")
	if g := term.Cell(0, 0); g.Mode != AttrBlink|AttrGfx || g.Char != '─' {
		t.Fatalf("cell %q mode %b, want ─ with AttrBlink|AttrGfx", g.Char, g.Mode)
	}
	write(t, term, "\x1b[0m\x1b[Habcd")
	if g := term.Cell(2, 0); g.Mode&AttrWrap == 0 {
		t.Fatalf("last cell before a wrap: mode %b, want AttrWrap", g.Mode)
	}
	write(t, term, "\x1b[2;3Hx")
	if c := term.Cursor(); c.State&CursorWrapNext == 0 {
		t.Fatalf("cursor state %b after writing the last column, want CursorWrapNext", c.State)
	}
	write(t, term, "\x1b[?6h")
	if c := term.Cursor(); c.State&CursorOrigin == 0 {
		t.Fatalf("cursor state %b after DECOM, want CursorOrigin", c.State)
	}
}
