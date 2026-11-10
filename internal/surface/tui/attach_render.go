package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
)

// The attach popup paints the emulator grid itself instead of going through
// lipgloss: a lipgloss style per cell (plus Render per run) was 3/4 of a
// popup frame. glyphPainter turns a cell's colors and attributes into one
// cached SGR prefix, and attachRowCache reuses a row's output while its
// cells are unchanged, which is most rows on most frames.

// attrMask is the Glyph.Mode bits the popup draws; others (wrap, gfx,
// blink) must not split runs.
const attrMask = vt10x.AttrBold | vt10x.AttrItalic | vt10x.AttrUnderline | vt10x.AttrReverse

// sgrKey is everything about a cell that changes how it is drawn.
type sgrKey struct {
	fg, bg vt10x.Color
	mode   int16 // attrMask bits only
}

// maxSGRCache bounds the prefix cache; an agent cycling through truecolor
// gradients could otherwise grow it without limit.
const maxSGRCache = 4096

// glyphPainter renders emulator cells for one color profile.
type glyphPainter struct {
	profile termenv.Profile
	prefix  map[sgrKey]string // SGR that starts a run; "" draws plain
}

func newGlyphPainter(profile termenv.Profile) *glyphPainter {
	return &glyphPainter{profile: profile, prefix: make(map[sgrKey]string)}
}

// sgr returns the escape sequence that starts a run drawn as k.
func (p *glyphPainter) sgr(k sgrKey) string {
	if s, ok := p.prefix[k]; ok {
		return s
	}
	if len(p.prefix) >= maxSGRCache {
		clear(p.prefix)
	}
	s := p.buildSGR(k)
	p.prefix[k] = s
	return s
}

func (p *glyphPainter) buildSGR(k sgrKey) string {
	if p.profile == termenv.Ascii {
		return "" // no styling at all, as lipgloss does for this profile
	}
	params := make([]string, 0, 6)
	if k.mode&vt10x.AttrBold != 0 {
		params = append(params, "1")
	}
	if k.mode&vt10x.AttrItalic != 0 {
		params = append(params, "3")
	}
	if k.mode&vt10x.AttrUnderline != 0 {
		params = append(params, "4")
	}
	if k.mode&vt10x.AttrReverse != 0 {
		params = append(params, "7")
	}
	if s := p.color(k.fg).Sequence(false); s != "" {
		params = append(params, s)
	}
	if s := p.color(k.bg).Sequence(true); s != "" {
		params = append(params, s)
	}
	if len(params) == 0 {
		return ""
	}
	return termenv.CSI + strings.Join(params, ";") + "m"
}

// color maps a vt10x color to the profile: 0–15 ANSI, 16–255 the 256-color
// cube, anything else 24-bit RGB (vt10x stores it as r<<16 | g<<8 | b).
func (p *glyphPainter) color(c vt10x.Color) termenv.Color {
	switch {
	case c == vt10x.DefaultFG || c == vt10x.DefaultBG || c == vt10x.DefaultCursor:
		return termenv.NoColor{}
	case c < 16:
		return p.profile.Convert(termenv.ANSIColor(c))
	case c < 256:
		return p.profile.Convert(termenv.ANSI256Color(c))
	default:
		return p.profile.Convert(termenv.RGBColor(fmt.Sprintf("#%06x", uint32(c)&0xFFFFFF)))
	}
}

const sgrReset = termenv.CSI + termenv.ResetSeq + "m"

// row renders cells as one line. The cell at cursorX (if any) is drawn with
// its reverse attribute flipped, so the cursor shows on reversed text too.
//
// vt10x stores a reversed cell with FG and BG already swapped and keeps the
// reverse bit. The painter swaps them back and sends SGR 7, so the terminal
// does the reversing once: drawing the swapped colors with SGR 7 inverted
// colored text twice, and drawing them without it lost reverse on
// default-colored text (an agent's own cursor).
func (p *glyphPainter) row(cells []vt10x.Glyph, cursorX int) string {
	var out strings.Builder
	out.Grow(len(cells) * 2)
	var prev sgrKey
	cur, open := "", false
	for x, g := range cells {
		k := sgrKey{fg: g.FG, bg: g.BG, mode: g.Mode & attrMask}
		if k.mode&vt10x.AttrReverse != 0 {
			k.fg, k.bg = k.bg, k.fg
		}
		if x == cursorX {
			k.mode ^= vt10x.AttrReverse
		}
		if x == 0 || k != prev {
			if pre := p.sgr(k); x == 0 || pre != cur {
				if open {
					out.WriteString(sgrReset)
				}
				out.WriteString(pre)
				cur, open = pre, pre != ""
			}
			prev = k
		}
		r := g.Char
		if r == 0 {
			r = ' '
		}
		out.WriteRune(r)
	}
	if open {
		out.WriteString(sgrReset)
	}
	return out.String()
}

// attachRowCache is one popup row's last rendered output and the cells and
// cursor column it was rendered from.
type attachRowCache struct {
	cells   []vt10x.Glyph
	cursorX int
	out     string
	valid   bool
}

// painter returns the popup's painter, created for the current lipgloss
// color profile on first use.
func (a *AttachState) painter() *glyphPainter {
	if a.paint == nil {
		a.paint = newGlyphPainter(lipgloss.ColorProfile())
	}
	return a.paint
}

// liveRow renders emulator row y, reusing the cached output when no cell
// and not the cursor column changed. Term lock must be held.
func (a *AttachState) liveRow(y, cols, cursorX int) string {
	if y >= len(a.rowCache) {
		a.rowCache = append(a.rowCache, make([]attachRowCache, y+1-len(a.rowCache))...)
	}
	c := &a.rowCache[y]
	from := 0
	if c.valid && c.cursorX == cursorX && len(c.cells) == cols {
		for from < cols && a.Term.Cell(from, y) == c.cells[from] {
			from++
		}
		if from == cols {
			return c.out
		}
	} else {
		from = 0
		if cap(c.cells) < cols {
			c.cells = make([]vt10x.Glyph, cols)
		}
		c.cells = c.cells[:cols]
	}
	for x := from; x < cols; x++ {
		c.cells[x] = a.Term.Cell(x, y)
	}
	c.out = a.painter().row(c.cells, cursorX)
	c.cursorX = cursorX
	c.valid = true
	return c.out
}
