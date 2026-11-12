package theme

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAccentForms(t *testing.T) {
	for in, want := range map[string]string{
		// Names: any case, spaces, '_' or '-'.
		"very-peri": "#6667AB", "Very Peri": "#6667AB", "  VERY_PERI ": "#6667AB",
		"living-coral": "#FF6F61", "peach fuzz": "#FFBE98", "mocha-mousse": "#A47864",
		// Hex.
		"#ff6f61": "#FF6F61", "#FF6F61": "#FF6F61", "#f0f": "#FF00FF", " #F0F ": "#FF00FF",
		// rgb(): 0-255, percentages, commas or spaces, mixed.
		"rgb(255, 111, 97)": "#FF6F61", "RGB(255,111,97)": "#FF6F61", "rgb(255 111 97)": "#FF6F61",
		"rgb(100%, 0%, 0%)": "#FF0000", "rgb(50%, 0, 255)": "#8000FF", "rgb( 0 , 0 , 0 )": "#000000",
		"rgb(127.5, 0, 0)": "#800000",
		// hsl(): hue wraps, % optional on s and l, deg allowed on h.
		"hsl(0, 100%, 50%)": "#FF0000", "hsl(120, 100, 50)": "#00FF00", "hsl(240deg 100% 50%)": "#0000FF",
		"hsl(360, 100%, 50%)": "#FF0000", "hsl(-120, 100%, 50%)": "#0000FF", "hsl(0, 0%, 100%)": "#FFFFFF",
		// cmyk(): percentages, fractions when every plain value is at most 1.
		"cmyk(0, 56, 62, 0)": "#FF7061", "cmyk(0%, 56%, 62%, 0%)": "#FF7061", "cmyk(0, .56, .62, 0)": "#FF7061",
		"cmyk(0, 0, 0, 100)": "#000000", "cmyk(1, 0, 0, 0)": "#00FFFF", "cmyk(0,0,0,0)": "#FFFFFF",
		// oklch(): CSS's red, black and white.
		"oklch(0.62796 0.25768 29.234)": "#FF0000", "oklch(0 0 0)": "#000000", "oklch(100% 0 0)": "#FFFFFF",
	} {
		c, err := ParseAccent(in)
		require.NoError(t, err, in)
		require.Equal(t, want, c.Dark, in)
		if c.Ref == "custom" {
			require.Equal(t, c.Dark, c.Light, in)
			require.Equal(t, strings.ToLower(strings.TrimSpace(in)), c.ID, "a custom value keeps its text: %q", in)
		}
	}
}

func TestParseAccentDefault(t *testing.T) {
	for _, in := range []string{"", "theme", " THEME ", "\t"} {
		c, err := ParseAccent(in)
		require.NoError(t, err, "%q", in)
		require.Empty(t, c.Dark, "%q is the theme's own accent", in)
	}
}

func TestParseAccentRefuses(t *testing.T) {
	for in, msg := range map[string]string{
		"nope":                "unknown accent color",
		"#12":                 "not #rgb or #rrggbb",
		"#12345":              "not #rgb or #rrggbb",
		"#gggggg":             "not #rgb or #rrggbb",
		"#1234567":            "not #rgb or #rrggbb",
		"rgb(1, 2)":           "takes 3 values, got 2",
		"rgb(1, 2, 3, 4)":     "takes 3 values, got 4",
		"rgb(256, 0, 0)":      "out of range 0-255",
		"rgb(-1, 0, 0)":       "out of range",
		"rgb(101%, 0, 0)":     "out of range 0-100",
		"rgb(0x10, 0, 0)":     "not a number",
		"rgb(nan, 0, 0)":      "not a number",
		"rgb(inf, 0, 0)":      "not a number",
		"rgb(1e999, 0, 0)":    "not a number",
		"rgb(1, 2, 3":         "unknown accent color",
		"rgb(10deg, 0, 0)":    "only a hue takes deg",
		"hsl(10%, 50%, 50%)":  "a hue is in degrees",
		"hsl(0, 101%, 50%)":   "out of range",
		"cmyk(0, 0, 0, 101)":  "out of range 0-100",
		"cmyk(0, 0, 0)":       "takes 4 values",
		"oklch(2, 0.1, 30)":   "out of range 0-1",
		"oklch(0.5, 0.6, 30)": "out of range 0-0.5",
		"lab(50, 20, 30)":     "unknown color function",
		"\x1b[31m":            "unknown accent color",
		"rgb()":               "takes 3 values, got 0",
	} {
		_, err := ParseAccent(in)
		require.Error(t, err, "%q", in)
		require.Contains(t, err.Error(), msg, "%q", in)
		require.NotContains(t, err.Error(), "\x1b", "no escape reaches the screen")
	}
	_, err := ParseAccent(strings.Repeat("x", 100))
	require.Error(t, err)
	require.Contains(t, err.Error(), "...", "a long value is cut")
}

func TestAccentColorsTable(t *testing.T) {
	require.GreaterOrEqual(t, len(AccentColors), 12)
	require.LessOrEqual(t, len(AccentColors), 16)
	seen := map[string]bool{}
	for _, c := range AccentColors {
		require.False(t, seen[c.ID], "duplicate %s", c.ID)
		seen[c.ID] = true
		require.NotEqual(t, AccentDefault, c.ID)
		require.Empty(t, c.ANSI, "an accent override paints its hex")
		for _, h := range []string{c.Dark, c.Light} {
			_, ok := hexRGB(h)
			require.True(t, ok, "%s %q", c.ID, h)
		}
		for _, e := range EdgeColors {
			if e.ID == c.ID {
				require.Equal(t, e.Dark, c.Dark, "%s reuses the border swatch", c.ID)
				require.Equal(t, e.Light, c.Light, c.ID)
			}
		}
		got, err := ParseAccent(c.ID)
		require.NoError(t, err)
		require.Equal(t, c, got)
	}
	require.Equal(t, len(AccentColors), len(AccentIDs()))
}

// accentProbes are colors no theme makes readable as they are: black,
// white, near-background grays and every named color.
func accentProbes() []string {
	return append([]string{"#000000", "#FFFFFF", "#101010", "#F8F8F8", "#808080", "#0000FF", "#FFFF00"}, AccentIDs()...)
}

// The override always reads on the theme's own background, dark or light.
func TestWithAccentContrast(t *testing.T) {
	var dark, light int
	for _, p := range hexThemes() {
		if p.Dark {
			dark++
		} else {
			light++
		}
		for _, v := range accentProbes() {
			q, res, err := WithAccent(p, v)
			require.NoError(t, err)
			require.Equal(t, res.Final, q.Accent)
			require.GreaterOrEqual(t, Contrast(q.Accent, p.Bg), AccentMinContrast, "%s on %s", v, p.Name)
			require.Equal(t, res.Chosen != res.Final, res.Adjusted, "%s on %s", v, p.Name)
			if Contrast(res.Chosen, p.Bg) >= AccentMinContrast {
				require.False(t, res.Adjusted, "%s on %s needs no change", v, p.Name)
			}
			q.Accent = p.Accent
			require.Equal(t, p, q, "only the accent changes")
		}
	}
	require.Positive(t, dark)
	require.Positive(t, light)
}

// A light theme darkens, a dark one lightens, and the hue stays.
func TestWithAccentAdjustDirection(t *testing.T) {
	def, _ := Get("default")
	paper, _ := Get("paper")
	_, res, err := WithAccent(def, "ultra-violet") // #5F4B8B is too dark on #0F1115
	require.NoError(t, err)
	require.True(t, res.Adjusted)
	before, _ := hexRGB(res.Chosen)
	after, _ := hexRGB(res.Final)
	require.Greater(t, after.toOklch().l, before.toOklch().l, "lightened on dark")
	require.InDelta(t, before.toOklch().h, after.toOklch().h, 8, "hue kept")

	_, res, err = WithAccent(paper, "peach-fuzz") // #FFBE98 is too light on #FBFAF7
	require.NoError(t, err)
	require.True(t, res.Adjusted)
	before, _ = hexRGB(res.Chosen)
	after, _ = hexRGB(res.Final)
	require.Less(t, after.toOklch().l, before.toOklch().l, "darkened on light")
	require.InDelta(t, before.toOklch().h, after.toOklch().h, 8, "hue kept")
	require.Contains(t, res.Note("paper"), "adjusted to "+res.Final+" for contrast on paper")

	_, res, err = WithAccent(def, "cerulean") // readable as is
	require.NoError(t, err)
	require.False(t, res.Adjusted)
	require.Equal(t, "#9BB7D4", res.Final)
	require.Empty(t, res.Note("default"))
}

func TestWithAccentVariantByMode(t *testing.T) {
	def, _ := Get("default")
	paper, _ := Get("paper")
	_, d, _ := WithAccent(def, "classic-blue")
	_, l, _ := WithAccent(paper, "classic-blue")
	require.Equal(t, "#4F83C0", d.Chosen, "the dark variant on a dark theme")
	require.Equal(t, "#0F4C81", l.Chosen, "the light variant on a light theme")
}

// Close to a state color: kept, and reported (TestStateHuesDistinct's 20).
func TestWithAccentClash(t *testing.T) {
	def, _ := Get("default")
	_, res, err := WithAccent(def, def.Working)
	require.NoError(t, err)
	require.Equal(t, "working", res.Clash)
	require.Equal(t, def.Working, res.Final, "a clash is a warning, not a change")
	require.Contains(t, res.Note("default"), "close to the working state color")
	for _, s := range []string{def.Needs, def.Done, def.Failed} {
		_, res, _ = WithAccent(def, s)
		require.NotEmpty(t, res.Clash, s)
	}
	_, res, _ = WithAccent(def, "cerulean")
	require.Empty(t, res.Clash)
	require.GreaterOrEqual(t, DeltaE("#9BB7D4", def.Working), float64(AccentClashDeltaE))
}

func TestWithAccentDefaultAndInvalid(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		for _, v := range []string{"", "theme"} {
			q, res, err := WithAccent(p, v)
			require.NoError(t, err)
			require.Equal(t, p, q, "%s: theme keeps today's look", name)
			require.Equal(t, AccentResult{}, res)
		}
		q, _, err := WithAccent(p, "nope")
		require.Error(t, err)
		require.Equal(t, p, q, "an invalid value keeps the theme's accent")
	}
}

// ansi leaves the background to the terminal: nothing to measure, so the
// color is used as given.
func TestWithAccentOnAnsi(t *testing.T) {
	p, _ := Get("ansi")
	q, res, err := WithAccent(p, "#101010")
	require.NoError(t, err)
	require.Equal(t, "#101010", q.Accent)
	require.False(t, res.Adjusted)
}

// The override sits on any theme: bundled, a user file and a Lua theme.
func TestWithAccentOnUserAndLuaThemes(t *testing.T) {
	user, _, err := Parse("mine", []byte("extends: paper\ncolors:\n  accent: \"#123456\"\n"))
	require.NoError(t, err)
	cat := (&Catalog{}).WithLua(map[string]Palette{"lunar": user}, nil)
	for _, name := range []string{"dracula", "tokyo_night", "pantone_safe", "lunar"} {
		p, ok := cat.Get(name)
		require.True(t, ok, name)
		q, res, err := WithAccent(p, "living-coral")
		require.NoError(t, err)
		require.Equal(t, res.Final, q.Accent, name)
		require.NotEqual(t, p.Accent, q.Accent, name)
		require.GreaterOrEqual(t, Contrast(q.Accent, p.Bg), AccentMinContrast, name)
	}
}

func TestDeltaE(t *testing.T) {
	require.InDelta(t, 0, DeltaE("#123456", "#123456"), 1e-9)
	require.InDelta(t, 100, DeltaE("#000000", "#FFFFFF"), 0.01)
	require.Equal(t, -1.0, DeltaE("5", "#FFFFFF"))
	require.InDelta(t, deltaE("#6EA8FE", "#B392F0"), DeltaE("#6EA8FE", "#B392F0"), 1e-9, "matches the test helper")
}

func TestOklchRoundTrip(t *testing.T) {
	for _, h := range []string{"#FF6F61", "#6667AB", "#0F4C81", "#F5DF4D", "#000000", "#FFFFFF", "#939597"} {
		c, _ := hexRGB(h)
		require.Equal(t, h, c.toOklch().rgb().hex(), h)
	}
	// Out of gamut: chroma gives way, lightness stays.
	o := oklch{0.7, 0.5, 150}
	c := o.rgb()
	require.InDelta(t, 0.7, c.toOklch().l, 0.02)
	require.False(t, math.IsNaN(c.r))
}
