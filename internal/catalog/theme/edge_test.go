package theme

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Non-text contrast (WCAG 2.x SC 1.4.11): at least 3:1 against the theme
// background. Dark variants against every dark theme, Light against paper.
func TestEdgeContrast(t *testing.T) {
	for _, p := range hexThemes() {
		for _, c := range EdgeColors {
			require.GreaterOrEqual(t, Contrast(c.Hex(p.Dark), p.Bg), 3.0, "%s on %s", c.ID, p.Name)
		}
	}
}

func TestEdgeTableWellFormed(t *testing.T) {
	require.Len(t, EdgeColors, 10)
	seen := map[string]bool{}
	for _, c := range EdgeColors {
		require.False(t, seen[c.ID], c.ID)
		seen[c.ID] = true
		require.True(t, isHex(c.Dark) && isHex(c.Light), c.ID)
		require.NotEmpty(t, c.Ref)
		require.NotEmpty(t, c.ANSI)
	}
	require.Len(t, EdgeIDs(), 10)
	for _, id := range append(EdgePool(true), EdgePool(false)...) {
		require.True(t, seen[id], "pool id %s", id)
	}
	// EdgePool returns a copy.
	EdgePool(true)[0] = "x"
	require.NotEqual(t, "x", EdgePool(true)[0])
	require.Panics(t, func() { poolColor("nope") })
}

// Machado, Oliveira and Fernandes 2009, severity 1.0, applied in linear sRGB.
var cvdMatrices = map[string][3][3]float64{
	"none":   {{1, 0, 0}, {0, 1, 0}, {0, 0, 1}},
	"protan": {{0.152286, 1.052583, -0.204868}, {0.114503, 0.786281, 0.099216}, {-0.003882, -0.048116, 1.051998}},
	"deutan": {{0.367322, 0.860646, -0.227968}, {0.280085, 0.672501, 0.047413}, {-0.011820, 0.042940, 0.968881}},
	"tritan": {{1.255528, -0.076749, -0.178779}, {-0.078411, 0.930809, 0.147602}, {0.004733, 0.691367, 0.303900}},
}

func linear(c float64) float64 {
	c /= 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// oklabUnder is the OKLab color of hex as seen with the given vision.
func oklabUnder(hex, vision string) [3]float64 {
	r8, g8, b8 := rgb(hex)
	in := [3]float64{linear(r8), linear(g8), linear(b8)}
	m := cvdMatrices[vision]
	var o [3]float64
	for i := range o {
		o[i] = math.Max(0, math.Min(1, m[i][0]*in[0]+m[i][1]*in[1]+m[i][2]*in[2]))
	}
	l := math.Cbrt(0.4122214708*o[0] + 0.5363325363*o[1] + 0.0514459929*o[2])
	mm := math.Cbrt(0.2119034982*o[0] + 0.6806995451*o[1] + 0.1073969566*o[2])
	s := math.Cbrt(0.0883024619*o[0] + 0.2817188376*o[1] + 0.6299787005*o[2])
	return [3]float64{
		0.2104542553*l + 0.7936177850*mm - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*mm + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*mm - 0.8086757660*s,
	}
}

// deltaEOK is the OKLab distance times 100.
func deltaEOK(a, b, vision string) float64 {
	x, y := oklabUnder(a, vision), oklabUnder(b, vision)
	return 100 * math.Sqrt((x[0]-y[0])*(x[0]-y[0])+(x[1]-y[1])*(x[1]-y[1])+(x[2]-y[2])*(x[2]-y[2]))
}

func mustEdge(t *testing.T, id string) EdgeColor {
	t.Helper()
	c, err := ParseEdge(id)
	require.NoError(t, err)
	return c
}

func TestEdgePoolCVDSeparation(t *testing.T) {
	for _, dark := range []bool{true, false} {
		pool := EdgePool(dark)
		for vision := range cvdMatrices {
			for i := range pool {
				for j := i + 1; j < len(pool); j++ {
					a, b := mustEdge(t, pool[i]).Hex(dark), mustEdge(t, pool[j]).Hex(dark)
					require.GreaterOrEqual(t, deltaEOK(a, b, vision), 6.5, "dark=%v %s vs %s under %s", dark, pool[i], pool[j], vision)
				}
			}
		}
	}
}

func TestEdgePoolANSI256Distinct(t *testing.T) {
	for _, dark := range []bool{true, false} {
		seen := map[string]string{}
		for _, id := range EdgePool(dark) {
			idx := ansi256Index(mustEdge(t, id).Hex(dark))
			require.NotContains(t, seen, idx, "%s collides with %s in 256 colors (dark=%v)", id, seen[idx], dark)
			seen[idx] = id
		}
	}
}

func TestPickEdgeDeterministicAndAvoid(t *testing.T) {
	a := PickEdge("/work/proj", true, nil)
	require.Equal(t, a, PickEdge("/work/proj", true, nil))
	require.Contains(t, EdgePool(true), a.ID)
	require.Contains(t, EdgePool(false), PickEdge("/work/proj", false, nil).ID)
	used := map[string]bool{}
	for _, s := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p"} {
		used[PickEdge(s, true, nil).ID] = true
	}
	require.Greater(t, len(used), 3)

	b := PickEdge("/work/proj", true, []string{a.ID})
	require.NotEqual(t, a.ID, b.ID)
	require.Contains(t, EdgePool(true), b.ID)
	require.Equal(t, a, PickEdge("/work/proj", true, EdgePool(true)), "avoiding everything falls back to the plain pick")
}

func TestNextEdgeCyclesPool(t *testing.T) {
	for _, dark := range []bool{true, false} {
		pool := EdgePool(dark)
		for i, id := range pool {
			require.Equal(t, pool[(i+1)%len(pool)], NextEdge(id, dark).ID)
		}
		require.Equal(t, pool[0], NextEdge("emerald", dark).ID, "outside the pool starts over")
		require.Equal(t, pool[0], NextEdge("#ff00ff", dark).ID)
	}
}

func TestParseEdge(t *testing.T) {
	c, err := ParseEdge("  Tangerine ")
	require.NoError(t, err)
	require.Equal(t, "tangerine", c.ID)
	c, err = ParseEdge("#ABC")
	require.NoError(t, err)
	require.Equal(t, EdgeColor{ID: "#aabbcc", Ref: "custom", Dark: "#aabbcc", Light: "#aabbcc"}, c)
	c, err = ParseEdge("#FF00ff")
	require.NoError(t, err)
	require.Equal(t, "#ff00ff", c.ID)
	require.Equal(t, "#ff00ff", c.Hex(true))
	require.Equal(t, "#ff00ff", c.Hex(false))

	for _, bad := range []string{"", "\x1b[31m", "#12", "#12345", "#gggggg", "#12345678", "rgb(1,2,3)", "nope", strings.Repeat("a", 65)} {
		_, err := ParseEdge(bad)
		require.Error(t, err, "%q", bad)
		require.NotContains(t, err.Error(), "\x1b")
		require.Contains(t, err.Error(), "tangerine")
	}
	_, err = ParseEdge(strings.Repeat("a", 65))
	require.Less(t, len(err.Error()), 300)
}
