package theme

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestParseMap_SameResultAsParse(t *testing.T) {
	yamlP, yamlW, err := Parse("mine", []byte("extends: noir\ncolors: {accent: '#b392f0', fleet: ['#111111', 7]}\nglyphs: {done: '+'}\nbogus: 1\n"))
	require.NoError(t, err)
	mapP, mapW, err := ParseMap("mine", map[string]any{
		"extends": "noir",
		"colors":  map[string]any{"accent": "#b392f0", "fleet": []any{"#111111", 7}},
		"glyphs":  map[string]any{"done": "+"},
		"bogus":   1,
	})
	require.NoError(t, err)
	require.Equal(t, yamlP, mapP)
	require.Equal(t, yamlW, mapW)
}

func TestParseMap_NameRules(t *testing.T) {
	_, _, err := ParseMap("noir", nil)
	require.ErrorContains(t, err, "built-in")
	_, _, err = ParseMap("bad name!", nil)
	require.Error(t, err)
	p, _, err := ParseMap("ok", nil)
	require.NoError(t, err)
	require.Equal(t, "ok", p.Name)
}

func FuzzParseMap(f *testing.F) {
	f.Add("extends: noir\ncolors: {accent: '#fff'}")
	f.Add("glyphs: {done: x}")
	f.Fuzz(func(t *testing.T, doc string) {
		var raw map[string]any
		if !utf8.ValidString(doc) || yaml.Unmarshal([]byte(doc), &raw) != nil {
			return
		}
		pY, wY, errY := Parse("fz", []byte(doc))
		pM, wM, errM := ParseMap("fz", raw)
		require.Equal(t, errY == nil, errM == nil)
		require.Equal(t, pY, pM)
		require.Equal(t, wY, wM)
	})
}
