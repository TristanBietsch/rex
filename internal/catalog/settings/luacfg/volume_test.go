package luacfg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Lua numbers carry no int/float distinction, so only a fraction from 0 to
// 1 reads as the old 0.0–1.0 scale; whole numbers (1 and 1.0 alike) are the
// 0–50 scale.
func TestEvalSet_MasterVolumeLegacyFloat(t *testing.T) {
	cases := map[string]int{"0.8": 40, "0.5": 25, "0.01": 1, "1": 1, "1.0": 1, "0": 0, "0.0": 0, "40": 40, "50": 50}
	for in, want := range cases {
		r := evalScript(t, "rex.set('master_volume', "+in+")")
		require.NoError(t, r.Err, in)
		require.Empty(t, r.Warnings, in)
		require.Equal(t, want, r.Values["master_volume"], in)
	}
	for _, in := range []string{"1.5", "12.5", "-0.2", "51", "0/0"} {
		r := evalScript(t, "rex.set('master_volume', "+in+")")
		require.NoError(t, r.Err, in)
		require.Len(t, r.Warnings, 1, in)
		require.NotContains(t, r.Values, "master_volume", in)
	}
}

// Given keeps the Lua number rex.set was passed, before Validate, so rex
// doctor can tell a config.lua that still writes the old scale (0.8, or 1
// meaning full) from one that writes 40. A later non-number, a refused
// call or a non-numeric setting leaves no entry.
func TestEvalSet_GivenKeepsTheLuaNumber(t *testing.T) {
	r := evalScript(t, "rex.set('master_volume', 0.8)")
	require.Equal(t, 40, r.Values["master_volume"])
	require.InDelta(t, 0.8, r.Given["master_volume"], 0)

	r = evalScript(t, "rex.set('master_volume', 1.0)")
	require.Equal(t, 1, r.Values["master_volume"])
	require.InDelta(t, 1.0, r.Given["master_volume"], 0)

	r = evalScript(t, "rex.set('master_volume', 0.8)\nrex.set('master_volume', 12.5)")
	require.Equal(t, 40, r.Values["master_volume"], "a refused call keeps the earlier value")
	require.InDelta(t, 0.8, r.Given["master_volume"], 0, "and its number")

	r = evalScript(t, "rex.set('theme', 'noir')")
	require.NotContains(t, r.Given, "theme")
}
