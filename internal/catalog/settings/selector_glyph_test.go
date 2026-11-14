package settings

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSelectorGlyphAcceptsThemeAndOneGlyph(t *testing.T) {
	for _, g := range []string{"theme", "▸", "☞", "👉", ">", "❯"} {
		st := NewStore()
		require.NoError(t, st.Set("selector_glyph", g), "%q", g)
		require.Equal(t, g, st.Get("selector_glyph"))
	}
	for name, g := range map[string]string{
		"empty": "", "space": " ", "two chars": "ab", "escape": "\x1b", "word": "themes",
	} {
		st := NewStore()
		require.Error(t, st.Set("selector_glyph", g), name)
		require.Equal(t, "theme", st.Get("selector_glyph"), name)
	}
}

// The presets are 1 cell wide, and only the finger (a user decision, U7 D1)
// is East Asian ambiguous width: the rest draw the same in every terminal.
func TestSelectorGlyphPresets(t *testing.T) {
	s, ok := Find("selector_glyph")
	require.True(t, ok)
	require.True(t, s.Free)
	require.Len(t, s.Options, 6, "theme plus five glyphs")
	require.Equal(t, "theme", s.Options[0])
	for _, g := range s.Options[1:] {
		require.Equal(t, 1, ansi.StringWidth(g), g)
		_, err := normalizeSelectorGlyph(g)
		require.NoError(t, err, g)
	}
	require.Contains(t, s.Options, "☞")
}

func TestSelectorNumericBounds(t *testing.T) {
	st := NewStore()
	require.Equal(t, 2, st.Get("selector_gap"))
	require.Error(t, st.Set("selector_gap", 0))
	require.Error(t, st.Set("selector_gap", 4))
	require.Equal(t, 600, st.Get("selector_blink_ms"))
	require.Error(t, st.Set("selector_blink_ms", 100))
	require.NoError(t, st.Set("selector_blink", "pulse"))
	require.Error(t, st.Set("selector_blink", "strobe"))
}
