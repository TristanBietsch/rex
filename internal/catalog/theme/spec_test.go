package theme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse_ExtendsOverridesAndGlyphs(t *testing.T) {
	p, warns, err := Parse("mine", []byte(`
extends: noir
dark: false
colors:
  accent: "#ff7a59"
  needs: "#F2B"
  working: 33
  info: ""
  fleet: ["#112233", "208"]
glyphs:
  needs: "!"
  done: "✓"
  select: ">"
`))
	require.NoError(t, err)
	require.Empty(t, warns)
	require.Equal(t, "mine", p.Name)
	require.False(t, p.Dark)
	require.Equal(t, "#FF7A59", p.Accent)
	require.Equal(t, "#FF22BB", p.Needs)
	require.Equal(t, "33", p.Working)
	require.Equal(t, "", p.Info)
	require.Equal(t, []string{"#112233", "208"}, p.Fleet)
	require.Equal(t, Glyphs{Needs: "!", Done: "✓", Select: ">"}, p.Glyphs)
	require.Equal(t, Builtin["noir"].Fg, p.Fg, "unset tokens come from the base")
	require.Equal(t, Builtin["noir"].Queued, p.Queued)
}

func TestParse_EmptyFileIsTheDefaultTheme(t *testing.T) {
	p, warns, err := Parse("blank", nil)
	require.NoError(t, err)
	require.Empty(t, warns)
	want := Builtin[DefaultName]
	want.Name = "blank"
	require.Equal(t, want, p)
}

func TestParse_DoesNotAliasTheBuiltin(t *testing.T) {
	p, _, err := Parse("mine", []byte("colors: {fleet: [\"1\"]}"))
	require.NoError(t, err)
	p.Fleet[0] = "x"
	q, _, err := Parse("other", nil)
	require.NoError(t, err)
	require.Equal(t, Builtin[DefaultName].Fleet, q.Fleet)
}

func TestParse_Errors(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"bad yaml":           {"colors: [", "invalid YAML"},
		"not utf8":           {"colors: {}\n# \xff", "UTF-8"},
		"extends not string": {"extends: 3", `"extends" must be`},
		"extends unknown":    {"extends: nope", `"nope" is not a built-in`},
		"extends user theme": {"extends: mine", `"mine" is not a built-in`},
		"dark not bool":      {"dark: maybe", `"dark" must be`},
		"colors not map":     {"colors: [a]", `"colors" must be`},
		"glyphs not map":     {"glyphs: x", `"glyphs" must be`},
		"bad hex":            {`colors: {accent: "#GGGGGG"}`, "colors.accent: "},
		"short hex":          {`colors: {accent: "#12"}`, "not #RGB or #RRGGBB"},
		"ansi out of range":  {`colors: {accent: "256"}`, "colors.accent"},
		"ansi int range":     {`colors: {accent: 300}`, "not 0-255"},
		"ansi negative":      {`colors: {accent: -1}`, "not 0-255"},
		"ansi leading zero":  {`colors: {accent: "007"}`, "colors.accent"},
		"color name":         {`colors: {accent: "red"}`, "colors.accent"},
		"unquoted hash":      {"colors:\n  accent: #FF7A59\n", "put the color in quotes"},
		"color is bool":      {"colors: {accent: true}", "not a color"},
		"fleet not list":     {`colors: {fleet: "#fff"}`, "colors.fleet: must be a list"},
		"fleet empty":        {"colors: {fleet: []}", "colors.fleet: needs 1-16"},
		"fleet too long":     {"colors: {fleet: [" + strings.Repeat(`"1",`, 17) + "]}", "colors.fleet: needs 1-16"},
		"fleet bad color":    {`colors: {fleet: ["#fff", "zz"]}`, "colors.fleet[1]"},
		"glyph not string":   {"glyphs: {needs: 5}", "glyphs.needs: must be a quoted"},
		"glyph escape":       {"glyphs: {needs: \"\\e[31m\"}", "glyphs.needs"},
		"glyph bidi":         {"glyphs: {needs: \"\\u202E\"}", "bidirectional"},
		"glyph wide":         {"glyphs: {needs: \"abc\"}", "one character, got 3"},
		"glyph empty":        {`glyphs: {needs: ""}`, "one printable character"},
		"glyph space":        {`glyphs: {needs: " "}`, "one printable character"},
		"glyph zero width":   {"glyphs: {needs: \"\\u0301\"}", "exactly 1 cell wide"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Parse("mine", []byte(tc.src))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestParse_Name(t *testing.T) {
	_, _, err := Parse("noir", nil)
	require.ErrorContains(t, err, "built-in")
	for _, bad := range []string{"", "Mine", "a b", "../x", strings.Repeat("a", MaxNameLen+1)} {
		_, _, err := Parse(bad, nil)
		require.Error(t, err, bad)
	}
}

func TestParse_SizeCap(t *testing.T) {
	big := []byte("# " + strings.Repeat("x", MaxFileSize) + "\n")
	_, _, err := Parse("mine", big)
	require.ErrorContains(t, err, "larger than 64 KiB")
}

func TestParse_UnknownKeysWarn(t *testing.T) {
	p, warns, err := Parse("mine", []byte(`
sparkle: true
colors: {accent: "#FFFFFF", glitter: "#fff"}
glyphs: {needs: "!", wobble: "?"}
`))
	require.NoError(t, err)
	require.Equal(t, "#FFFFFF", p.Accent)
	require.Equal(t, []string{
		`unknown color token "glitter" (ignored)`,
		`unknown glyph token "wobble" (ignored)`,
		`unknown key "sparkle" (ignored)`,
	}, warns)
}

func TestParse_LowContrastWarnsButLoads(t *testing.T) {
	p, warns, err := Parse("dim", []byte(`
extends: noir
colors: {fg: "#222222", fg_dim: "#222222", accent: "#111111"}
`))
	require.NoError(t, err)
	require.Equal(t, "#222222", p.Fg)
	require.NotEmpty(t, warns)
	require.Contains(t, strings.Join(warns, "\n"), "low contrast: fg #222222")
	require.Contains(t, strings.Join(warns, "\n"), "low contrast: accent")
}

func TestParse_NoBackgroundNoContrastWarning(t *testing.T) {
	_, warns, err := Parse("mine", []byte("extends: ansi\ncolors: {accent: \"#000000\"}\n"))
	require.NoError(t, err)
	require.Empty(t, warns)
}

func TestTokens(t *testing.T) {
	require.Contains(t, ColorTokens(), "accent")
	require.Contains(t, ColorTokens(), "fleet")
	require.Equal(t, []string{"crashed", "done", "failed", "loop", "needs", "queued", "select"}, GlyphTokens())
}

func TestValidGlyph(t *testing.T) {
	for _, ok := range []string{"!", "✓", "◆", "x", "●", "é", ">"} {
		require.NoError(t, ValidGlyph(ok), ok)
	}
	for _, bad := range []string{"", " ", "\x1b", "\x1b[31m", "\u202e", "\u200b", "ab", "\xff", "\n", "\u0085", "\u200f", "\u2066"} {
		require.Error(t, ValidGlyph(bad), "%q", bad)
	}
	for _, wide := range []string{"✅", "👉", "👍", "👨‍👩‍👧", "日"} {
		err := ValidGlyph(wide)
		require.ErrorContains(t, err, "exactly 1 cell wide", wide)
		require.ErrorContains(t, err, "is 2", wide)
	}
}

func TestParse_WideGlyphIsRejectedWithAReason(t *testing.T) {
	for _, g := range []string{"✅", "👉"} {
		_, _, err := Parse("mine", []byte("glyphs: {needs: \""+g+"\"}"))
		require.ErrorContains(t, err, "glyphs.needs: must be exactly 1 cell wide")
	}
}

func TestNormalizeColor(t *testing.T) {
	for in, want := range map[string]string{"": "", "#abc": "#AABBCC", "#AbCdEf": "#ABCDEF", "0": "0", "255": "255"} {
		got, err := NormalizeColor(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got)
	}
}

func TestParse_NullBlocksAreEmpty(t *testing.T) {
	p, warns, err := Parse("mine", []byte("extends: noir\ncolors:\n# accent: \"#fff\"\nglyphs:\n"))
	require.NoError(t, err)
	require.Empty(t, warns)
	require.Equal(t, Builtin["noir"].Accent, p.Accent)
}

func TestTokenLists(t *testing.T) {
	p := Builtin["noir"]
	colors := p.ColorList()
	require.Len(t, colors, len(colorOrder))
	require.Equal(t, Token{Name: "accent", Value: p.Accent}, colors[7])
	for _, tok := range colors {
		require.Contains(t, ColorTokens(), tok.Name)
	}
	require.Len(t, colors, len(ColorTokens())-1, "every color token but fleet is listed")

	p.Glyphs = Glyphs{Needs: "!", Select: ">"}
	glyphs := p.GlyphList()
	require.Equal(t, []Token{
		{"needs", "!"}, {"done", ""}, {"failed", ""}, {"crashed", ""}, {"queued", ""}, {"select", ">"}, {"loop", ""},
	}, glyphs)
	require.Len(t, glyphs, len(GlyphTokens()))
}
