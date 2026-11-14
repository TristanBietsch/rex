package theme

import (
	"math"
	"strconv"
	"testing"

	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func isHex(s string) bool { return len(s) == 7 && s[0] == '#' }

// hexThemes are the built-in and bundled themes that give real colors (every
// one but ansi).
func hexThemes() []Palette {
	var out []Palette
	for _, name := range Names() {
		if p, _ := Get(name); isHex(p.Bg) {
			out = append(out, p)
		}
	}
	return out
}

func TestContrastKnownValues(t *testing.T) {
	require.InDelta(t, 21.0, Contrast("#FFFFFF", "#000000"), 0.01)
	require.InDelta(t, 1.0, Contrast("#123456", "#123456"), 0.001)
	require.InDelta(t, Contrast("#FFFFFF", "#000000"), Contrast("#000000", "#FFFFFF"), 1e-9, "symmetric")
	require.Zero(t, Contrast("5", "#000000"), "an ANSI index has no measurable color")
	require.Zero(t, Contrast("", "#000000"))
	require.Zero(t, Contrast("#12345", "#000000"))
	require.Zero(t, Contrast("#ZZZZZZ", "#000000"))
}

// The legibility floor every theme with real colors meets against its own
// assumed background.
func TestBuiltinContrast(t *testing.T) {
	require.NotEmpty(t, hexThemes())
	for _, p := range hexThemes() {
		at := func(token, fg string, min float64) {
			require.GreaterOrEqual(t, Contrast(fg, p.Bg), min, "%s %s %s on %s", p.Name, token, fg, p.Bg)
		}
		at("Fg", p.Fg, 7)
		at("FgDim", p.FgDim, 4.5)
		at("Accent", p.Accent, 4.5)
		at("Working", p.Working, 4.5)
		at("Needs", p.Needs, 4.5)
		at("Done", p.Done, 4.5)
		at("Failed", p.Failed, 4.5)
		at("BorderFocus", p.BorderFocus, 3)
		at("FgMuted", p.FgMuted, 2)
		require.Less(t, Contrast(p.FgMuted, p.Bg), Contrast(p.FgDim, p.Bg), "%s: muted recedes behind dim", p.Name)
		require.GreaterOrEqual(t, Contrast(p.Fg, p.BgSel), 7.0, "%s: text on the selection", p.Name)
	}
}

func rgb(s string) (r, g, b float64) {
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		panic(err)
	}
	return float64(v >> 16 & 0xFF), float64(v >> 8 & 0xFF), float64(v & 0xFF)
}

// lab converts #RRGGBB (sRGB, D65) to CIE L*a*b*.
func lab(s string) (lum, a, b float64) {
	lin := func(c float64) float64 {
		c /= 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r8, g8, b8 := rgb(s)
	r, g, bl := lin(r8), lin(g8), lin(b8)
	x := (0.4124*r + 0.3576*g + 0.1805*bl) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*bl
	z := (0.0193*r + 0.1192*g + 0.9505*bl) / 1.08883
	f := func(t float64) float64 {
		if t > 216.0/24389 {
			return math.Cbrt(t)
		}
		return (24389.0/27*t + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

func deltaE(a, b string) float64 {
	l1, a1, b1 := lab(a)
	l2, a2, b2 := lab(b)
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

// ansi256Index is the 256-color index a hex value falls back to on a
// terminal without truecolor.
func ansi256Index(hex string) string {
	return termenv.ANSI256.Convert(termenv.RGBColor(hex)).(termenv.ANSI256Color).Sequence(false)
}

// The state colors (and the accent, which must not read as "working") stay
// apart, and still do after a 256-color terminal quantizes them.
func TestStateHuesDistinct(t *testing.T) {
	for _, p := range hexThemes() {
		colors := map[string]string{"Working": p.Working, "Needs": p.Needs, "Done": p.Done, "Failed": p.Failed}
		if p.Name != "classic" { // classic paints the accent with the working blue, as it always did
			colors["Accent"] = p.Accent
		}
		names := make([]string, 0, len(colors))
		for n := range colors {
			names = append(names, n)
		}
		for i := range names {
			for j := i + 1; j < len(names); j++ {
				a, b := names[i], names[j]
				require.GreaterOrEqual(t, deltaE(colors[a], colors[b]), 20.0, "%s: %s vs %s", p.Name, a, b)
				require.NotEqual(t, ansi256Index(colors[a]), ansi256Index(colors[b]), "%s: %s vs %s in 256 colors", p.Name, a, b)
			}
		}
		text := []string{p.Fg, p.FgDim, p.FgMuted}
		for i := range text {
			for j := i + 1; j < len(text); j++ {
				require.NotEqual(t, ansi256Index(text[i]), ansi256Index(text[j]), "%s: text tones in 256 colors", p.Name)
			}
		}
	}
}

// ansi borrows the terminal's own colors. It uses only the six normal hues
// (1-6): the bright variants and index 8 are not safe, because palettes such
// as Solarized map them to background and gray shades (8 is the Solarized
// Dark background). Neutral tones are the terminal default drawn faint, and
// the selection is reverse video.
func TestAnsiThemeIndices(t *testing.T) {
	p := Builtin["ansi"]
	require.True(t, p.SelReverse)
	require.True(t, p.Faint)
	require.False(t, isHex(p.Fg))
	for name, v := range map[string]string{
		"Accent": p.Accent, "Working": p.Working, "Queued": p.Queued, "Needs": p.Needs,
		"Failed": p.Failed, "Warn": p.Warn, "Danger": p.Danger, "Info": p.Info,
	} {
		n, err := strconv.Atoi(v)
		require.NoError(t, err, "ansi.%s=%q is an index", name, v)
		require.True(t, n >= 1 && n <= 6, "ansi.%s=%d is a normal hue (1-6), never black, gray or bright", name, n)
	}
	for name, v := range map[string]string{
		"Fg": p.Fg, "FgDim": p.FgDim, "FgMuted": p.FgMuted, "Border": p.Border, "BorderFocus": p.BorderFocus,
		"BgSel": p.BgSel, "Crashed": p.Crashed,
	} {
		require.Empty(t, v, "ansi.%s is the terminal default (dim ones are drawn faint)", name)
	}
	require.NotEmpty(t, p.Fleet)
	for _, f := range p.Fleet {
		n, err := strconv.Atoi(f)
		require.NoError(t, err)
		require.True(t, n >= 1 && n <= 6, "fleet color %d", n)
	}
	require.Equal(t, "1", p.Failed)
	// Ready is the green, Done the bright green: the one bright index. A
	// terminal that maps 10 to a gray (Solarized) dims the finished state,
	// never the one that needs you; the ✓ marker still says done.
	require.Equal(t, "2", p.Needs)
	require.Equal(t, "10", p.Done)
	require.Equal(t, p.Done, p.Success, "success follows done (quota OK, check marks)")
	require.Equal(t, "4", p.Working)
	require.Equal(t, "5", p.Accent)
}

func TestFaintOnlyOnAnsi(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		require.Equal(t, name == "ansi", p.Faint, name)
	}
}

func TestSelReverseOnlyOnAnsi(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		require.Equal(t, name == "ansi", p.SelReverse, name)
	}
}

// oklchHue is the OKLCH hue angle of a "#RRGGBB" color, in degrees.
func oklchHue(hex string) float64 {
	c, _ := hexRGB(hex)
	return c.toOklch().h
}

// Ready and Done are both green in every theme: a bright "go" green for Ready
// and a calmer one for Done. They stay 20 apart (CIE76), and each reads at
// 4.5:1 on the theme's background (TestBuiltinContrast); the markers differ
// too (⚑ and ✓), so a reader who cannot tell the greens apart still can.
func TestReadyDoneGreens(t *testing.T) {
	for _, p := range hexThemes() {
		for token, c := range map[string]string{"Needs": p.Needs, "Done": p.Done} {
			h := oklchHue(c)
			require.True(t, h >= 115 && h <= 170, "%s %s %s is green (OKLCH hue %.0f)", p.Name, token, c, h)
			require.GreaterOrEqual(t, Contrast(c, p.Bg), 4.5, "%s %s %s on %s", p.Name, token, c, p.Bg)
		}
		// Board rows have no selection background, but list pages do: the
		// markers stay visible there too (3:1, the non-text floor).
		require.GreaterOrEqual(t, Contrast(p.Needs, p.BgSel), 3.0, "%s Needs on BgSel", p.Name)
		require.GreaterOrEqual(t, Contrast(p.Done, p.BgSel), 3.0, "%s Done on BgSel", p.Name)
		require.GreaterOrEqual(t, deltaE(p.Needs, p.Done), 20.0, "%s: ready vs done", p.Name)
		require.NotEqual(t, ansi256Index(p.Needs), ansi256Index(p.Done), "%s: ready vs done in 256 colors", p.Name)
		// Ready is the brighter of the two on a dark theme and the more
		// saturated one everywhere: it is the green that asks for you.
		rc, _ := hexRGB(p.Needs)
		dc, _ := hexRGB(p.Done)
		require.Greater(t, rc.toOklch().c, dc.toOklch().c, "%s: ready is more saturated than done", p.Name)
		if p.Dark {
			require.Greater(t, Contrast(p.Needs, p.Bg), Contrast(p.Done, p.Bg), "%s: ready is brighter than done", p.Name)
		}
		require.Equal(t, p.Done, p.Success, "%s: success follows done", p.Name)
		// The greens differ in lightness too, which survives color-vision
		// deficiency: the pair stays apart under simulated protan, deutan
		// and tritan vision, by the U4 edge pool's 6.5.
		for vision := range cvdMatrices {
			require.GreaterOrEqual(t, deltaEOK(p.Needs, p.Done, vision), 6.5, "%s: ready vs done under %s", p.Name, vision)
		}
	}
}
