package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// AccentDefault is the accent_color value that keeps the active theme's own
// accent.
const AccentDefault = "theme"

// AccentMinContrast is the contrast an accent override must reach against
// the theme's background, the floor TestBuiltinContrast holds every
// shipped accent to. A weaker color is lightened (dark theme) or darkened
// (light theme) in OKLCH until it passes.
const AccentMinContrast = 4.5

// AccentClashDeltaE is the CIE76 distance under which an accent reads as a
// state color (TestStateHuesDistinct keeps the shipped accents 20 away).
const AccentClashDeltaE = 20

// AccentColors are the named accent colors, in hue order. Like EdgeColors
// they are Pantone Color of the Year inspired: the hex values are published
// sRGB approximations, not official Pantone values, and rex is not affiliated
// with Pantone. The names shared with EdgeColors reuse its dark and light
// variants; the rest use one value, and the contrast check adapts it.
var AccentColors = []EdgeColor{
	edgeByID("very-peri"),    // 17-3938 Very Peri
	edgeByID("classic-blue"), // 19-4052 Classic Blue
	edgeByID("cerulean"),     // 15-4020 Cerulean
	edgeByID("emerald"),      // 17-5641 Emerald
	{"greenery", "15-0343 Greenery", "#88B04B", "#88B04B", ""},
	edgeByID("illuminating"), // 13-0647 Illuminating
	edgeByID("mimosa"),       // 14-0848 Mimosa
	edgeByID("tangerine"),    // 17-1463 Tangerine Tango
	{"living-coral", "16-1546 Living Coral", "#FF6F61", "#FF6F61", ""},
	{"peach-fuzz", "13-1023 Peach Fuzz", "#FFBE98", "#FFBE98", ""},
	{"rose-quartz", "13-1520 Rose Quartz", "#F7CAC9", "#F7CAC9", ""},
	edgeByID("viva-magenta"), // 18-1750 Viva Magenta
	edgeByID("orchid"),       // 18-3224 Radiant Orchid
	{"ultra-violet", "18-3838 Ultra Violet", "#5F4B8B", "#5F4B8B", ""},
	{"mocha-mousse", "17-1230 Mocha Mousse", "#A47864", "#A47864", ""},
	edgeByID("ultimate-gray"), // 17-5104 Ultimate Gray
}

// edgeByID is the EdgeColors entry id, without its ANSI index: an accent
// override always paints its hex value.
func edgeByID(id string) EdgeColor {
	c := poolColor(id)
	c.ANSI = ""
	return c
}

// AccentIDs returns the named accent colors in table order.
func AccentIDs() []string {
	ids := make([]string, len(AccentColors))
	for i, c := range AccentColors {
		ids[i] = c.ID
	}
	return ids
}

// accentFormats is the help shown with a refused value.
func accentFormats() string {
	return "theme, one of " + strings.Join(AccentIDs(), ", ") + ", #rrggbb, rgb(r, g, b), hsl(h, s%, l%), cmyk(c, m, y, k) or oklch(l c h)"
}

// ParseAccent reads an accent_color value, case-insensitive and
// whitespace-tolerant: "theme" (or "") is the theme's own accent and returns
// a zero EdgeColor; a name from AccentColors returns that entry; any other
// accepted form (#rgb, #rrggbb, rgb(), hsl(), cmyk(), oklch()) returns a
// custom color whose ID is the value as given and whose Dark and Light are
// the same "#RRGGBB". Everything else is an error, so only a validated hex
// ever reaches a terminal color.
func ParseAccent(s string) (EdgeColor, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == AccentDefault {
		return EdgeColor{}, nil
	}
	name := strings.Join(strings.FieldsFunc(v, func(r rune) bool { return r == '_' || r == '-' || unicode.IsSpace(r) }), "-")
	for _, c := range AccentColors {
		if c.ID == name {
			return c, nil
		}
	}
	hex, err := parseColorExpr(v)
	if err != nil {
		return EdgeColor{}, err
	}
	return EdgeColor{ID: v, Ref: "custom", Dark: hex, Light: hex}, nil
}

// parseColorExpr parses a hex or functional color to "#RRGGBB".
func parseColorExpr(v string) (string, error) {
	if strings.HasPrefix(v, "#") {
		h := strings.Join(strings.Fields(v[1:]), "")
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) == 6 && isLowerHex(h) {
			return "#" + strings.ToUpper(h), nil
		}
		return "", fmt.Errorf("%s is not #rgb or #rrggbb", quoteValue(v))
	}
	open := strings.IndexByte(v, '(')
	if open < 0 || !strings.HasSuffix(v, ")") {
		return "", fmt.Errorf("unknown accent color %s: use %s", quoteValue(v), accentFormats())
	}
	fn := strings.TrimSpace(v[:open])
	args := strings.FieldsFunc(v[open+1:len(v)-1], func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	want := map[string]int{"rgb": 3, "hsl": 3, "cmyk": 4, "oklch": 3}
	n, ok := want[fn]
	if !ok {
		return "", fmt.Errorf("unknown color function %s: use rgb(), hsl(), cmyk() or oklch()", quoteValue(fn))
	}
	if len(args) != n {
		return "", fmt.Errorf("%s() takes %d values, got %d", fn, n, len(args))
	}
	nums := make([]number, n)
	for i, a := range args {
		x, err := parseNumber(a)
		if err != nil {
			return "", fmt.Errorf("%s(): %w", fn, err)
		}
		nums[i] = x
	}
	var c rgb8
	var err error
	switch fn {
	case "rgb":
		c, err = rgbFrom(nums)
	case "hsl":
		c, err = hslFrom(nums)
	case "cmyk":
		c, err = cmykFrom(nums)
	default:
		c, err = oklchFrom(nums)
	}
	if err != nil {
		return "", fmt.Errorf("%s(): %w", fn, err)
	}
	return c.hex(), nil
}

// number is one function argument: its value and whether it ended in %
// (or "deg", which only a hue may carry).
type number struct {
	v        float64
	pct, deg bool
	text     string
}

func parseNumber(s string) (number, error) {
	n := number{text: s}
	switch {
	case strings.HasSuffix(s, "%"):
		n.pct, s = true, strings.TrimSuffix(s, "%")
	case strings.HasSuffix(s, "deg"):
		n.deg, s = true, strings.TrimSuffix(s, "deg")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || strings.ContainsAny(s, "xXpP_") {
		return n, fmt.Errorf("%s is not a number", quoteValue(n.text))
	}
	n.v = v
	return n, nil
}

// inRange is x as a fraction 0-1: a percentage over 100, else over max.
func (x number) inRange(maxPlain float64) (float64, error) {
	if x.deg {
		return 0, fmt.Errorf("%s: only a hue takes deg", quoteValue(x.text))
	}
	lim, f := maxPlain, x.v/maxPlain
	if x.pct {
		lim, f = 100, x.v/100
	}
	if x.v < 0 || x.v > lim {
		return 0, fmt.Errorf("%s is out of range 0-%g", quoteValue(x.text), lim)
	}
	return f, nil
}

func (x number) hue() (float64, error) {
	if x.pct {
		return 0, fmt.Errorf("%s: a hue is in degrees, not %%", quoteValue(x.text))
	}
	return x.v, nil
}

// rgbFrom: each channel 0-255 or 0-100%.
func rgbFrom(n []number) (rgb8, error) {
	var ch [3]float64
	for i := range ch {
		f, err := n[i].inRange(255)
		if err != nil {
			return rgb8{}, err
		}
		ch[i] = f * 255
	}
	return rgb8{ch[0], ch[1], ch[2]}, nil
}

// hslFrom: hue in degrees (any value; it wraps), saturation and lightness
// 0-100 with or without %.
func hslFrom(n []number) (rgb8, error) {
	h, err := n[0].hue()
	if err != nil {
		return rgb8{}, err
	}
	s, err := n[1].inRange(100)
	if err != nil {
		return rgb8{}, err
	}
	l, err := n[2].inRange(100)
	if err != nil {
		return rgb8{}, err
	}
	return hslRGB(h, s, l), nil
}

// cmykFrom: inks 0-100%, or plain numbers that are fractions 0-1 when all
// plain ones are at most 1 and percentages 0-100 otherwise.
func cmykFrom(n []number) (rgb8, error) {
	maxPlain := 1.0
	for _, x := range n {
		if !x.pct && x.v > 1 {
			maxPlain = 100
		}
	}
	var ink [4]float64
	for i := range ink {
		f, err := n[i].inRange(maxPlain)
		if err != nil {
			return rgb8{}, err
		}
		ink[i] = f
	}
	return cmykRGB(ink[0], ink[1], ink[2], ink[3]), nil
}

// oklchFrom: lightness 0-1 or 0-100%, chroma 0-0.5 (100% is 0.4, as in CSS)
// and hue in degrees. A color outside sRGB loses chroma until it fits.
func oklchFrom(n []number) (rgb8, error) {
	l, err := n[0].inRange(1)
	if err != nil {
		return rgb8{}, err
	}
	c := n[1]
	var chroma float64
	if c.pct {
		f, err := c.inRange(0)
		if err != nil {
			return rgb8{}, err
		}
		chroma = f * 0.4
	} else {
		if chroma, err = c.inRange(0.5); err != nil {
			return rgb8{}, err
		}
		chroma *= 0.5
	}
	h, err := n[2].hue()
	if err != nil {
		return rgb8{}, err
	}
	return oklch{l, chroma, h}.rgb(), nil
}

// quoteValue quotes a value for an error message: control characters
// replaced and long values cut, since the message reaches the screen.
func quoteValue(s string) string {
	if r := []rune(s); len(r) > 32 {
		s = string(r[:32]) + "..."
	}
	return strconv.Quote(printable(s))
}

// AccentResult says how an accent_color value was applied to a palette.
type AccentResult struct {
	// Chosen is the color the value names for this theme ("#RRGGBB"); ""
	// when the theme keeps its own accent.
	Chosen string
	// Final is what is painted: Chosen, or the contrast-adjusted color.
	Final string
	// Adjusted is true when Final differs from Chosen for contrast.
	Adjusted bool
	// Clash names the state color ("working", "needs", "done", "failed")
	// Final is too close to, or "".
	Clash string
}

// WithAccent returns p with its accent replaced by the accent_color value
// (see ParseAccent). "theme" or "" leaves p unchanged. On a theme with a
// "#RRGGBB" background the color must reach AccentMinContrast against it,
// else it is lightened (dark background) or darkened (light) in OKLCH until
// it does, keeping its hue. A color too close to a state color is kept and
// reported in Clash. An invalid value returns p unchanged and the error.
func WithAccent(p Palette, value string) (Palette, AccentResult, error) {
	c, err := ParseAccent(value)
	if err != nil || c.Dark == "" {
		return p, AccentResult{}, err
	}
	res := AccentResult{Chosen: c.Hex(p.Dark)}
	res.Final, res.Adjusted = readableOn(res.Chosen, p.Bg)
	p.Accent = res.Final
	states := []struct{ name, hex string }{{"working", p.Working}, {"needs", p.Needs}, {"done", p.Done}, {"failed", p.Failed}}
	for _, s := range states {
		if d := DeltaE(res.Final, s.hex); d >= 0 && d < AccentClashDeltaE {
			res.Clash = s.name
			break
		}
	}
	return p, res, nil
}

// readableOn moves hex's OKLCH lightness away from bg in small steps until
// the pair reaches AccentMinContrast. A bg that is not a hex color (the
// terminal decides it) leaves hex alone.
func readableOn(hex, bg string) (string, bool) {
	if Contrast(hex, bg) == 0 || Contrast(hex, bg) >= AccentMinContrast {
		return hex, false
	}
	c, _ := hexRGB(hex)
	o := c.toOklch()
	b, _ := hexRGB(bg)
	step := 0.01
	if b.toOklch().l > 0.5 {
		step = -0.01
	}
	best := hex
	for i := 0; i < 100; i++ {
		o.l += step
		if o.l < 0 || o.l > 1 {
			break
		}
		best = o.rgb().hex()
		if Contrast(best, bg) >= AccentMinContrast {
			return best, true
		}
	}
	return best, best != hex
}

// Note is the one-line notice for r on the theme named themeName, or "" when
// there is nothing to say.
func (r AccentResult) Note(themeName string) string {
	var parts []string
	if r.Adjusted {
		parts = append(parts, fmt.Sprintf("accent %s adjusted to %s for contrast on %s", r.Chosen, r.Final, themeName))
	}
	if r.Clash != "" {
		parts = append(parts, fmt.Sprintf("accent %s is close to the %s state color", r.Final, r.Clash))
	}
	return strings.Join(parts, "; ")
}
