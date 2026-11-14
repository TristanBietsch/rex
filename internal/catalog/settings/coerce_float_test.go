package settings

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// No registry setting is a float since master_volume moved to 0–50, but
// TypeFloat stays supported; this keeps its coercion covered.
func TestCoerce_Float(t *testing.T) {
	s := Setting{ID: "gain", Type: TypeFloat, Min: 0, Max: 2, MaxText: "full"}
	for raw, want := range map[any]float64{0.5: 0.5, 2: 2, "1.25": 1.25} {
		got, err := coerce(s, raw)
		require.NoError(t, err, raw)
		require.InDelta(t, want, got, 1e-9, raw)
	}
	for _, raw := range []any{2.5, -1, "x", true, "NaN", math.Inf(1)} {
		_, err := coerce(s, raw)
		require.Error(t, err, raw)
	}
	require.Equal(t, "full", s.Display(2.0))
	require.Equal(t, "1.5", s.Display(1.5))
	require.Empty(t, s.Display(nil))
}

func TestStore_DisplayUnknownID(t *testing.T) {
	require.Empty(t, NewStore().Display("no_such_setting"))
}
