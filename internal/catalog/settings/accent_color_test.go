package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccentColorDefault(t *testing.T) {
	st := NewStore()
	require.Equal(t, "theme", st.Get("accent_color"))
	s, ok := Find("accent_color")
	require.True(t, ok)
	require.Equal(t, SectionAppearance, s.Section)
	require.Equal(t, ApplyLive, s.Apply)
	require.True(t, s.Free)
	require.Equal(t, "theme", s.Options[0], "the theme's own accent comes first")
	require.Contains(t, s.Options, "very-peri")
}

// The value is kept as typed (trimmed, lowercased), names in their
// canonical spelling, so the settings row shows what was chosen.
func TestAccentColorNormalize(t *testing.T) {
	st := NewStore()
	for in, want := range map[string]string{
		"theme": "theme", " THEME ": "theme", "": "theme",
		"Very Peri": "very-peri", "LIVING_CORAL": "living-coral",
		"#FF6F61": "#ff6f61", " #F0F ": "#f0f",
		"rgb(255, 111, 97)": "rgb(255, 111, 97)", "HSL(5, 100%, 69%)": "hsl(5, 100%, 69%)",
		"cmyk(0, 56, 62, 0)": "cmyk(0, 56, 62, 0)", "oklch(0.7 0.15 30)": "oklch(0.7 0.15 30)",
	} {
		require.NoError(t, st.Set("accent_color", in), "%q", in)
		require.Equal(t, want, st.Get("accent_color"), "%q", in)
	}
	require.NoError(t, st.Set("accent_color", "#ff6f61"))
	for _, bad := range []string{"nope", "#12", "rgb(256, 0, 0)", "\x1b[31m", "#" + strings.Repeat("a", 70)} {
		err := st.Set("accent_color", bad)
		require.Error(t, err, "%q", bad)
		require.NotContains(t, err.Error(), "\x1b")
	}
	require.Equal(t, "#ff6f61", st.Get("accent_color"), "a refused value is not stored")
}
