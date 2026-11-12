package registry

import (
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/width"
)

// TestBuiltinGlyphsUnique: every built-in tool has its own icon, one cell
// wide, so the wizard and pickers can tell agents apart at a glance. One cell
// in every width helper: East Asian narrow or neutral (never ambiguous, which
// a CJK terminal draws two cells wide) and no emoji presentation.
func TestBuiltinGlyphsUnique(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	wide := runewidth.NewCondition()
	wide.EastAsianWidth = true
	seen := map[string]string{}
	for _, tool := range reg.Tools {
		require.NotEmpty(t, tool.Icon, tool.ID)
		require.Equal(t, 1, xansi.StringWidth(tool.Icon), "%s icon %q must be one cell wide", tool.ID, tool.Icon)
		require.Len(t, []rune(tool.Icon), 1, "%s icon %q is one code point (no variation selector)", tool.ID, tool.Icon)
		require.Equal(t, 1, uniseg.StringWidth(tool.Icon), "%s icon %q has an emoji presentation", tool.ID, tool.Icon)
		require.Equal(t, 1, wide.StringWidth(tool.Icon), "%s icon %q is East Asian ambiguous", tool.ID, tool.Icon)
		k := width.LookupRune([]rune(tool.Icon)[0]).Kind()
		require.True(t, k == width.Neutral || k == width.EastAsianNarrow, "%s icon %q kind %v", tool.ID, tool.Icon, k)
		if other, dup := seen[tool.Icon]; dup {
			t.Errorf("%s and %s share icon %q", other, tool.ID, tool.Icon)
		}
		seen[tool.Icon] = tool.ID
	}
}

// The icon set of v2.0.0, pinned so a change is deliberate (docs/registry.md
// lists the rationale per glyph).
func TestBuiltinGlyphSet(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	want := map[string]string{
		"echo": "∿", "claude": "✻", "codex": "❀", "antigravity": "✧", "gemini": "⁑",
		"ollama": "⌂", "qwen": "❖", "cursor": "◮", "grok": "⍁", "deepseek": "≋", "kimi": "☾",
	}
	got := map[string]string{}
	for _, tool := range reg.Tools {
		got[tool.ID] = tool.Icon
	}
	require.Equal(t, want, got)
}
