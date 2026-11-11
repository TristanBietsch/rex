package pty

import (
	"bytes"
	"strconv"

	"github.com/hinshun/vt10x"
)

// Repaint renders a screen as the escape sequences that redraw it: the
// visible cells with their colors and attributes, the primary screen under
// an active alternate screen, the terminal modes (as ModePreamble's and
// more), the scroll region, the saved and current cursor, the pen and the
// cursor's visibility. Fed to a fresh vt10x terminal of the same size, it
// reproduces v's cells, cursor, modes and scroll region (see
// repaint_test.go); fed to a real terminal, it draws the same picture.
//
// An attach client that subscribes without the transcript replay (the
// full-screen agents) has nothing on its screen until the agent draws
// again, and an agent that is busy or ignores the ^L nudge may not draw for
// a long time. The daemon sends it this instead (Subscribe.Repaint).
//
// It never scrolls or clears the whole screen: each row is drawn in place
// (CUP, cells, EL) with autowrap off, so a client's scrollback does not take
// the repaint as history. Rows are drawn cell by cell, as vt10x stores them
// (one cell per rune, wide runes included); on a real terminal a wide rune
// pushes the rest of its row right, as in the attach popup.
//
// Not reproduced: tab stops, the window title, palette changes (cells carry
// the colors they resolve to), keyboard-lock, local-echo and 8-bit-meta
// modes, and the wrap marks vt10x keeps on wrapped lines.
//
// The caller must hold v's lock.
func Repaint(v vt10x.View) []byte {
	cols, rows := v.Size()
	if cols <= 0 || rows <= 0 {
		return nil
	}
	mode := v.Mode()
	r := &repainter{pen: defaultPen}
	// A known starting state on any terminal: cursor hidden while drawing,
	// autowrap off, origin mode off, whole-screen scroll region, replace
	// mode, default pen and charset.
	r.b.WriteString("\x1b[?25l\x1b[?7l\x1b[?6l\x1b[r\x1b[4l\x1b(B\x1b[0m")
	if mode&vt10x.ModeAltScreen != 0 {
		// The primary screen first: it is what the agent returns to.
		// Entering the alternate screen saves the cursor, as it did for
		// the agent.
		r.rows(cols, rows, v.InactiveCell)
		r.savedCursor(v.SavedCursor(), "\x1b[?1049h")
		r.rows(cols, rows, v.Cell)
	} else {
		r.rows(cols, rows, v.Cell)
		r.savedCursor(v.SavedCursor(), "\x1b7")
	}
	r.modes(mode)
	r.cursor(v, rows)
	if mode&vt10x.ModeHide == 0 {
		r.b.WriteString("\x1b[?25h")
	}
	return r.b.Bytes()
}

// penModes are the Glyph.Mode bits a pen carries into the cells it writes.
const penModes = vt10x.AttrBold | vt10x.AttrItalic | vt10x.AttrUnderline |
	vt10x.AttrBlink | vt10x.AttrReverse | vt10x.AttrGfx

// pen is the terminal's current drawing state: SGR attributes and colors
// plus the line-drawing charset (AttrGfx).
type pen struct {
	mode   int16
	fg, bg vt10x.Color
}

var defaultPen = pen{fg: vt10x.DefaultFG, bg: vt10x.DefaultBG}

type repainter struct {
	b   bytes.Buffer
	pen pen // what the terminal holds; Repaint starts by resetting it
}

func (r *repainter) num(n int) { r.b.WriteString(strconv.Itoa(n)) }

// cup moves to 1-based row and column.
func (r *repainter) cup(row, col int) {
	r.b.WriteString("\x1b[")
	r.num(row)
	r.b.WriteByte(';')
	r.num(col)
	r.b.WriteByte('H')
}

// rows draws every row of one screen buffer in place.
func (r *repainter) rows(cols, rows int, cell func(x, y int) vt10x.Glyph) {
	for y := 0; y < rows; y++ {
		r.cup(y+1, 1)
		last := cols - 1
		for last >= 0 && isDefaultBlank(cell(last, y)) {
			last--
		}
		for x := 0; x <= last; x++ {
			r.glyph(cell(x, y), false)
		}
		// EL after a write to the last column would erase that cell, so
		// only rows that end in blanks get one.
		if last < cols-1 {
			r.setPen(defaultPen)
			r.b.WriteString("\x1b[K")
		}
	}
}

// isDefaultBlank reports whether g is what EL with the default pen leaves.
func isDefaultBlank(g vt10x.Glyph) bool {
	return (g.Char == ' ' || g.Char == 0) && g.Mode&^vt10x.AttrWrap == 0 &&
		g.FG == vt10x.DefaultFG && g.BG == vt10x.DefaultBG
}

// glyph draws g at the cursor and advances it. vt10x stores a written cell
// with its pen transformed (bold brightens colors 0-7, reverse swaps FG and
// BG) and an erased cell with the pen as is, so a cell is re-created by
// whichever path can produce it. wrap is set for the last column when a
// wrap must be left pending, which only a write does.
func (r *repainter) glyph(g vt10x.Glyph, wrap bool) {
	p, ok := writePen(g)
	if !ok && (g.Char == ' ' || g.Char == 0) && expressible(g.FG, g.BG) {
		// Erased under a pen no write reproduces (bold with colors 0-7,
		// reverse with default colors): erase it under the same pen.
		r.setPen(pen{mode: g.Mode & penModes, fg: g.FG, bg: g.BG})
		if wrap {
			r.b.WriteString(" \x1b[X") // the erase keeps the write's pending wrap
		} else {
			r.b.WriteString("\x1b[X\x1b[C")
		}
		return
	}
	r.setPen(p)
	r.b.WriteRune(printable(g.Char))
}

// writePen returns the pen whose write stores g, and whether one exists.
// Without one it returns the closest pen.
func writePen(g vt10x.Glyph) (pen, bool) {
	p := pen{mode: g.Mode & penModes, fg: g.FG, bg: g.BG}
	ok := true
	if p.mode&vt10x.AttrReverse != 0 {
		p.fg, p.bg = g.BG, g.FG
	} else if p.mode&vt10x.AttrBold != 0 && g.FG < 8 {
		ok = false // a bold write would store it brightened
	}
	if !expressible(p.fg, p.bg) {
		ok = false
		if !isColor(p.fg) {
			p.fg = vt10x.DefaultFG
		}
		if !isColor(p.bg) {
			p.bg = vt10x.DefaultBG
		}
	}
	return p, ok
}

func isColor(c vt10x.Color) bool { return c < 1<<24 }

// expressible reports whether SGR can set fg and bg as the pen's colors.
func expressible(fg, bg vt10x.Color) bool {
	return (fg == vt10x.DefaultFG || isColor(fg)) && (bg == vt10x.DefaultBG || isColor(bg))
}

// printable keeps control characters out of the repaint: vt10x stores them
// as cells while the line-drawing charset is on; a real terminal would act
// on them.
func printable(c rune) rune {
	if c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) {
		return ' '
	}
	return c
}

// setPen switches the terminal's pen to p.
func (r *repainter) setPen(p pen) {
	if p.mode&vt10x.AttrGfx != r.pen.mode&vt10x.AttrGfx {
		if p.mode&vt10x.AttrGfx != 0 {
			r.b.WriteString("\x1b(0")
		} else {
			r.b.WriteString("\x1b(B")
		}
	}
	if p.mode&^vt10x.AttrGfx == r.pen.mode&^vt10x.AttrGfx && p.fg == r.pen.fg && p.bg == r.pen.bg {
		r.pen = p
		return
	}
	r.pen = p
	r.b.WriteString("\x1b[0")
	for _, a := range []struct {
		bit int16
		sgr byte
	}{{vt10x.AttrBold, '1'}, {vt10x.AttrItalic, '3'}, {vt10x.AttrUnderline, '4'}, {vt10x.AttrBlink, '5'}, {vt10x.AttrReverse, '7'}} {
		if p.mode&a.bit != 0 {
			r.b.WriteByte(';')
			r.b.WriteByte(a.sgr)
		}
	}
	r.color(p.fg, 30, 90, 38)
	r.color(p.bg, 40, 100, 48)
	r.b.WriteByte('m')
}

// color appends c's SGR parameters: base+n for 0-7, bright+n-8 for 8-15,
// ext;5;n for 16-255 and ext;2;r;g;b for 24-bit colors (vt10x stores them
// as r<<16 | g<<8 | b). Default colors need none after the reset.
func (r *repainter) color(c vt10x.Color, base, bright, ext int) {
	switch {
	case !isColor(c):
	case c < 8:
		r.b.WriteByte(';')
		r.num(base + int(c))
	case c < 16:
		r.b.WriteByte(';')
		r.num(bright + int(c) - 8)
	case c < 256:
		r.b.WriteByte(';')
		r.num(ext)
		r.b.WriteString(";5;")
		r.num(int(c))
	default:
		r.b.WriteByte(';')
		r.num(ext)
		r.b.WriteString(";2;")
		r.num(int(c >> 16 & 0xff))
		r.b.WriteByte(';')
		r.num(int(c >> 8 & 0xff))
		r.b.WriteByte(';')
		r.num(int(c & 0xff))
	}
}

// savedCursor puts the saved cursor's position, pen and origin mode in
// place and saves it with save (DECSC, or entering the alternate screen).
// The scroll region is the whole screen here, so its position is absolute
// even in origin mode.
func (r *repainter) savedCursor(c vt10x.Cursor, save string) {
	origin := c.State&vt10x.CursorOrigin != 0
	if origin {
		r.b.WriteString("\x1b[?6h")
	}
	r.cup(c.Y+1, c.X+1)
	r.setPen(cursorPen(c))
	r.b.WriteString(save)
	if origin {
		r.b.WriteString("\x1b[?6l")
	}
}

// cursorPen is a cursor's pen. Unlike a cell, it holds the colors as set.
func cursorPen(c vt10x.Cursor) pen {
	p := pen{mode: c.Attr.Mode & penModes, fg: c.Attr.FG, bg: c.Attr.BG}
	if !isColor(p.fg) {
		p.fg = vt10x.DefaultFG
	}
	if !isColor(p.bg) {
		p.bg = vt10x.DefaultBG
	}
	return p
}

// modes sets every mode the repaint reproduces, on or off. The alternate
// screen was entered (or not) while drawing.
func (r *repainter) modes(m vt10x.ModeFlag) {
	set := func(on bool, seq string) {
		r.b.WriteString("\x1b[")
		r.b.WriteString(seq)
		if on {
			r.b.WriteByte('h')
		} else {
			r.b.WriteByte('l')
		}
	}
	set(m&vt10x.ModeWrap != 0, "?7")
	set(m&vt10x.ModeInsert != 0, "4")
	set(m&vt10x.ModeCRLF != 0, "20")
	set(m&vt10x.ModeReverse != 0, "?5")
	set(m&vt10x.ModeAppCursor != 0, "?1")
	if m&vt10x.ModeAppKeypad != 0 {
		r.b.WriteString("\x1b=")
	} else {
		r.b.WriteString("\x1b>")
	}
	r.b.WriteString("\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l")
	switch {
	case m&vt10x.ModeMouseMany != 0:
		r.b.WriteString("\x1b[?1003h")
	case m&vt10x.ModeMouseMotion != 0:
		r.b.WriteString("\x1b[?1002h")
	case m&vt10x.ModeMouseButton != 0:
		r.b.WriteString("\x1b[?1000h")
	case m&vt10x.ModeMouseX10 != 0:
		r.b.WriteString("\x1b[?9h")
	}
	set(m&vt10x.ModeMouseSgr != 0, "?1006")
	set(m&vt10x.ModeFocus != 0, "?1004")
}

// cursor restores the scroll region, origin mode, position, pending wrap
// and pen.
func (r *repainter) cursor(v vt10x.View, rows int) {
	top, bottom := v.ScrollRegion()
	if top != 0 || bottom != rows-1 {
		r.b.WriteString("\x1b[")
		r.num(top + 1)
		r.b.WriteByte(';')
		r.num(bottom + 1)
		r.b.WriteByte('r')
	}
	c := v.Cursor()
	y := c.Y
	if c.State&vt10x.CursorOrigin != 0 {
		r.b.WriteString("\x1b[?6h")
		y -= top
	}
	r.cup(y+1, c.X+1)
	if c.State&vt10x.CursorWrapNext != 0 {
		// Only a write to the last column leaves a wrap pending: rewrite
		// the cell that is already there.
		r.glyph(v.Cell(c.X, c.Y), true)
	}
	r.setPen(cursorPen(c))
}
