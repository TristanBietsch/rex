package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeScanColor(t *testing.T) {
	for in, want := range map[string]string{
		"": "accent", "accent": "accent", " Accent ": "accent", "theme": "accent",
		"living-coral": "living-coral", "Living Coral": "living-coral",
		"#FF6F61": "#ff6f61", "rgb(255, 111, 97)": "rgb(255, 111, 97)",
	} {
		got, err := normalizeScanColor(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"nope", "#12", "rgb(1,2)"} {
		_, err := normalizeScanColor(bad)
		require.Error(t, err, bad)
	}
}

func TestScanSettingsDefaults(t *testing.T) {
	st := NewStore()
	require.Equal(t, true, st.Get("summary_scan"))
	require.Equal(t, "accent", st.String("summary_scan_color"))
	require.Equal(t, true, st.Get("filter_chips"))
	require.Error(t, st.Set("summary_scan_color", "not-a-color"))
}
