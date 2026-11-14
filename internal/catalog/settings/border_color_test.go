package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowBorderDefaultsOff(t *testing.T) {
	st := NewStore()
	require.Equal(t, false, st.Get("window_border"))
	require.Equal(t, "random", st.Get("window_border_color"))
}

func TestWindowBorderColorNormalize(t *testing.T) {
	st := NewStore()
	for in, want := range map[string]string{
		"random": "random", "RANDOM": "random", "Tangerine": "tangerine",
		"#F0F": "#ff00ff", "#00FF7f": "#00ff7f",
	} {
		require.NoError(t, st.Set("window_border_color", in), in)
		require.Equal(t, want, st.Get("window_border_color"), in)
	}
	require.NoError(t, st.Set("window_border_color", "#00FF7f"))
	for _, bad := range []string{"", "\x1b[31m", "#12", "rgb(1,2,3)", "nope"} {
		require.Error(t, st.Set("window_border_color", bad), "%q", bad)
	}
	require.Equal(t, "#00ff7f", st.Get("window_border_color"), "a refused value is not stored")
}
