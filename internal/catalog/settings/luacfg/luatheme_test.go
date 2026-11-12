package luacfg

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalTheme_YAMLSchemaNested(t *testing.T) {
	r := evalScript(t, `
rex.theme("mine", {
  extends = "noir",
  dark = true,
  colors = { accent = "#b392f0", bg = "#0f1115", fleet = { "#111111", 7 } },
  glyphs = { done = "+" },
})
`)
	require.NoError(t, r.Err)
	p, ok := r.Themes["mine"]
	require.True(t, ok)
	require.Equal(t, "mine", p.Name)
	require.Equal(t, "#B392F0", p.Accent)
	require.Equal(t, "#0F1115", p.Bg)
	require.Equal(t, []string{"#111111", "7"}, p.Fleet)
	require.Equal(t, "+", p.Glyphs.Done)
	require.True(t, p.Dark)
	for _, i := range r.ThemeIssues {
		require.True(t, i.Warn, i.Msg)
	}
}

func TestEvalTheme_MissingTokensComeFromBaseSilently(t *testing.T) {
	r := evalScript(t, `rex.theme("tiny", { colors = { accent = "#FF0000" } })`)
	require.NoError(t, r.Err)
	require.Empty(t, r.Warnings)
	for _, i := range r.ThemeIssues {
		require.NotContains(t, i.Msg, "missing")
	}
	require.NotEmpty(t, r.Themes["tiny"].Fg)
}

func TestEvalTheme_Refusals(t *testing.T) {
	r := evalScript(t, `
rex.theme("noir", {})
rex.theme("bad name", {})
rex.theme("g1", { glyphs = { done = "\27[31m" } })
rex.theme("g2", { glyphs = { done = "ab" } })
rex.theme("g3", { glyphs = { done = "`+"\U0001F600"+`" } })
rex.theme("c1", { colors = { accent = "red" } })
rex.theme("c2", { colors = { accent = {} } })
rex.theme("e1", { extends = "nope" })
rex.theme("t1", { colors = { fleet = "x" } })
rex.theme("k1", { [1] = "x", colors = {} })
rex.theme("d1", { a = { b = { c = { d = 1 } } } })
rex.theme("f1", { colors = { accent = rex.set } })
rex.theme("s1", { colors = { accent = "\27[1m" } })
rex.theme("m1", { colors = { fleet = { "#111111", x = "#222222" } } })
`)
	require.NoError(t, r.Err)
	require.Empty(t, r.Themes)
	require.Len(t, r.ThemeIssues, 14)
	for _, i := range r.ThemeIssues {
		require.False(t, i.Warn)
		require.NotContains(t, i.Msg, "\x1b")
		require.Regexp(t, `^config\.lua:\d+$`, i.File)
	}
	require.Contains(t, r.ThemeIssues[0].Msg, "built-in")
	require.Contains(t, r.ThemeIssues[2].Msg, "control")
	require.Contains(t, r.ThemeIssues[4].Msg, "1 cell")
	require.Contains(t, r.ThemeIssues[10].Msg, "nested more than 3")
}

func TestEvalTheme_TooManyKeysAndEntries(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("local c = {}\nfor i = 1, 70 do c['k' .. i] = '#111111' end\nrex.theme('wide', { colors = c })\n")
	sb.WriteString("local f = {}\nfor i = 1, 70 do f[i] = '#111111' end\nrex.theme('long', { colors = { fleet = f } })\n")
	r := evalScript(t, sb.String())
	require.NoError(t, r.Err)
	require.Len(t, r.ThemeIssues, 2)
	require.Contains(t, r.ThemeIssues[0].Msg, "more than 64 keys")
	require.Contains(t, r.ThemeIssues[1].Msg, "more than 64 entries")
}

func TestEvalTheme_DuplicateAndLimit(t *testing.T) {
	r := evalScript(t, `
rex.theme("a", { colors = { accent = "#111111" } })
rex.theme("a", { colors = { accent = "#222222" } })
`)
	require.Equal(t, "#222222", r.Themes["a"].Accent)
	var dup bool
	for _, i := range r.ThemeIssues {
		if i.Warn && strings.Contains(i.Msg, "defined again") {
			dup = true
		}
	}
	require.True(t, dup)

	r = evalScript(t, "for i = 1, 70 do rex.theme('t' .. i, {}) end")
	require.Len(t, r.Themes, 64)
	require.Contains(t, r.Warnings[0], "more than 64 themes")
}

func TestEvalTheme_LowContrastIsAWarningIssue(t *testing.T) {
	r := evalScript(t, `rex.theme("dim", { colors = { bg = "#000000", fg = "#0a0a0a" } })`)
	require.Contains(t, r.Themes, "dim")
	require.NotEmpty(t, r.ThemeIssues)
	require.True(t, r.ThemeIssues[0].Warn)
}

func TestEvalTheme_BadArgsAreErrors(t *testing.T) {
	require.Error(t, evalScript(t, `rex.theme("x")`).Err)
	require.Error(t, evalScript(t, `rex.theme("x", 3)`).Err)
}
