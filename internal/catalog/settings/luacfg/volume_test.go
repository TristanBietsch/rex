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
