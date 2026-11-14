package settings

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// prompt_glyph is echoed on every frame, so Store.Set (the one validator the
// TUI editor, `rex config set` and rex setup share) accepts exactly one
// printable grapheme, one or two cells wide.
func TestPromptGlyphAcceptsOneGrapheme(t *testing.T) {
	for _, g := range []string{"λ", "›", "❯", "%", ">", "文", "é", "é", "🙂", "👍🏽", "⛬", "x"} {
		st := NewStore()
		require.NoError(t, st.Set("prompt_glyph", g), "%q", g)
		require.Equal(t, g, st.Get("prompt_glyph"))
	}
}

func TestPromptGlyphRejectsControlChars(t *testing.T) {
	for name, g := range map[string]string{
		"empty":         "",
		"escape":        "\x1b",
		"sgr":           "\x1b[31m",
		"newline":       "a\nb",
		"tab":           "\t",
		"nul":           "\x00",
		"c1":            "\u0085",
		"bidi override": "\u202e",
		"bidi isolate":  "\u2066",
		"zero width":    "\u200b",
		"space":         " ",
		"two chars":     "ab",
		"wide pair":     "文字",
		"invalid utf8":  "\xff",
		"line sep":      "\u2028",
	} {
		st := NewStore()
		err := st.Set("prompt_glyph", g)
		require.Error(t, err, name)
		require.Equal(t, "λ", st.Get("prompt_glyph"), "%s: a refused value is not stored", name)
	}
}

func TestPromptGlyphIsFreeForm(t *testing.T) {
	s, _ := Find("prompt_glyph")
	require.True(t, s.Free, "the options are suggestions; any glyph that passes Normalize is accepted")
	a, _ := Find("auto_trust_dirs")
	require.True(t, a.Free)
	for _, id := range []string{"default_spawn_tool", "default_spawn_model", "default_spawn_effort", "board_hidden_sections", "summary_model", "lua_config_path"} {
		o, _ := Find(id)
		require.False(t, o.Free, "%s is not free-form text", id)
	}
	for _, other := range Registry {
		if other.Free {
			require.Equal(t, TypeString, other.Type, other.ID)
			require.NotNil(t, other.Normalize, "%s: a free-form setting needs a validator", other.ID)
		}
	}
}

// The width check is the TUI's own (ansi.StringWidth), so a glyph the page
// accepts is never wider than the prompt layout believes. Emoji with a
// variation selector are the case the two libraries could disagree on.
func TestPromptGlyphWidthMatchesTheTUI(t *testing.T) {
	for _, g := range []string{"\u2764\ufe0f", "\u263a\ufe0f", "\u2714\ufe0f", "\u00a9\ufe0f", "1\ufe0f\u20e3", "\U0001F1E9\U0001F1EA", "\u2603", "\u2605"} {
		w := ansi.StringWidth(g)
		st := NewStore()
		err := st.Set("prompt_glyph", g)
		if w >= 1 && w <= 2 {
			require.NoError(t, err, "%q is %d cells wide", g, w)
		} else {
			require.Error(t, err, "%q is %d cells wide", g, w)
		}
	}
}
