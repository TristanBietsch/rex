package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/hinshun/vt10x"
	"github.com/muesli/termenv"
)

// The attach popup paints the emulator grid itself instead of going through
// lipgloss: a lipgloss style per cell (plus Render per run) was 3/4 of a
// popup frame. glyphPainter turns a cell's colors and attributes into one
// cached SGR prefix, and attachRowCache reuses a row's output while its
// cells are unchanged, which is most rows on most frames.

// glyphAttrs are the Glyph.Mode bits for the attributes the popup draws.
// vt10x keeps them unexported, so they are learned once from the emulator.
type glyphAttrs struct {
	bold, italic, underline, reverse int16
}

func (b glyphAttrs) mask() int16 { return b.bold | b.italic | b.underline | b.reverse }

var vtGlyphAttrs = sync.OnceValue(func() glyphAttrs {
	term := vt10x.New(vt10x.WithSize(4, 1))
	_, _ = term.Write([]byte("\x1b[1mA\x1b[0m\x1b[3mB\x1b[0m\x1b[4mC\x1b[0m\x1b[7mD\x1b[0m"))
	term.Lock()
	defer term.Unlock()
	return glyphAttrs{
		bold:      term.Cell(0, 0).Mode,
		italic:    term.Cell(1, 0).Mode,
		underline: term.Cell(2, 0).Mode,
		reverse:   term.Cell(3, 0).Mode,
	}
})

// sgrKey is everything about a cell that changes how it is drawn.
type sgrKey struct {
	fg, bg vt10x.Color
	mode   int16 // glyphAttrs bits only
}

// maxSGRCache bounds the prefix cache; an agent cycling through truecolor
// gradients could otherwise grow it without limit.
const maxSGRCache = 4096

// glyphPainter renders emulator cells for one color profile.
type glyphPainter struct {
	profile termenv.Profile
	attrs   glyphAttrs
	mask    int16
	prefix  map[sgrKey]string // SGR that starts a run; "" draws plain
}

func newGlyphPainter(profile termenv.Profile) *glyphPainter {
	a := vtGlyphAttrs()
	return &glyphPainter{profile: profile, attrs: a, mask: a.mask(), prefix: make(map[sgrKey]string)}
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
	if k.mode&p.attrs.bold != 0 {
		params = append(params, "1")
	}
	if k.mode&p.attrs.italic != 0 {
		params = append(params, "3")
	}
	if k.mode&p.attrs.underline != 0 {
		params = append(params, "4")
	}
	if k.mode&p.attrs.reverse != 0 {
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
func (p *glyphPainter) row(cells []vt10x.Glyph, cursorX int) string {
	var out strings.Builder
	out.Grow(len(cells) * 2)
	var prev sgrKey
	cur, open := "", false
	for x, g := range cells {
		k := sgrKey{fg: g.FG, bg: g.BG, mode: g.Mode & p.mask}
		if x == cursorX {
			k.mode ^= p.attrs.reverse
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
