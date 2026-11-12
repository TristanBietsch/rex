package theme

import (
	"math"
	"strconv"
	"testing"

	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"
)

func isHex(s string) bool { return len(s) == 7 && s[0] == '#' }

// hexThemes are the built-ins that give real colors (every one but ansi).
func hexThemes() []Palette {
	var out []Palette
	for _, name := range Names() {
		if p := Builtin[name]; isHex(p.Bg) {
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
		"Accent": p.Accent, "Working": p.Working, "Queued": p.Queued, "Needs": p.Needs, "Done": p.Done,
		"Failed": p.Failed, "Warn": p.Warn, "Danger": p.Danger, "Success": p.Success, "Info": p.Info,
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
	require.Equal(t, "2", p.Done)
	require.Equal(t, "3", p.Needs)
	require.Equal(t, "4", p.Working)
	require.Equal(t, "5", p.Accent)
}

func TestFaintOnlyOnAnsi(t *testing.T) {
	for _, name := range Names() {
		require.Equal(t, name == "ansi", Builtin[name].Faint, name)
	}
}

func TestSelReverseOnlyOnAnsi(t *testing.T) {
	for _, name := range Names() {
		require.Equal(t, name == "ansi", Builtin[name].SelReverse, name)
	}
}
