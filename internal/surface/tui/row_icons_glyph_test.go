package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/width"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var iconGlyphNames = []string{"unseen", "asks", "review", "loop", "subagents", "spend", "longrun"}

func iconModel(t *testing.T, kv ...any) Model {
	t.Helper()
	m := Model{Store: settings.NewStore()}
	for i := 0; i+1 < len(kv); i += 2 {
		require.NoError(t, m.Store.Set(kv[i].(string), kv[i+1]))
	}
	return m
}

// TestRowIconGlyphs: every strip glyph is one cell, East Asian narrow or
// neutral, and collides with no other board glyph.
func TestRowIconGlyphs(t *testing.T) {
	taken := map[string]bool{
		selectDefault: true, queuedGlyph: true, "▍": true, "∴": true, "λ": true,
		"↳": true, "»": true, "✎": true, "▰": true, "▱": true,
	}
	for _, st := range []protocol.State{
		protocol.StateNeedsInput, protocol.StateDone, protocol.StateFailed,
		protocol.StateCrashed, protocol.StateQueued, protocol.StateWorking,
	} {
		if g := stateGlyph(st); g != "" {
			taken[g] = true
		}
	}
	for _, set := range waitingSets {
		for _, f := range set.Frames {
			taken[f] = true
		}
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	for _, tool := range reg.Tools {
		if tool.Icon != "" {
			taken[tool.Icon] = true
		}
	}
	// Reused with the same meaning.
	reused := map[string]string{"⋔": subagentGlyph, "▴": "▴", "⟳": loopDefault}
	for g, want := range reused {
		require.Equal(t, want, g)
		delete(taken, g)
	}
	require.Equal(t, "▴", longRunGlyph)

	for _, ascii := range []bool{false, true} {
		m := iconModel(t, "ascii_glyphs", ascii)
		seen := map[string]string{}
		for _, name := range iconGlyphNames {
			g := iconGlyph(m, name)
			require.NotEmpty(t, g, name)
			require.Equal(t, 1, ansi.StringWidth(g), "%s %q", name, g)
			rs := []rune(g)
			require.Len(t, rs, 1, "%s %q", name, g)
			k := width.LookupRune(rs[0]).Kind()
			require.True(t, k == width.Neutral || k == width.EastAsianNarrow, "%s %q kind %v", name, g, k)
			require.False(t, taken[g], "%s glyph %q collides with a board glyph", name, g)
			if other, dup := seen[g]; dup {
				t.Fatalf("%s and %s share glyph %q", name, other, g)
			}
			seen[g] = name
		}
	}
	require.Equal(t, "✦", iconGlyph(iconModel(t), "unseen"))
	require.Equal(t, "*", iconGlyph(iconModel(t, "ascii_glyphs", true), "unseen"))
}

func TestIconSlots_SetAndFlags(t *testing.T) {
	all := []iconSlot{slotUnseen, slotReason, slotLoop, slotSubagents, slotSpend, slotLongRun}
	// loops defaults off; turn it on to see all six.
	m := iconModel(t, "loops", true)
	require.Equal(t, all, iconSlots(m))

	require.NoError(t, m.Store.Set("row_icon_set", "spend,unseen,bogus"))
	require.Equal(t, []iconSlot{slotUnseen, slotSpend}, iconSlots(m))

	require.NoError(t, m.Store.Set("row_icon_set", "unseen,reason,loop,subagents,spend,longrun"))
	require.NoError(t, m.Store.Set("loops", false))
	require.NotContains(t, iconSlots(m), slotLoop)
	require.NoError(t, m.Store.Set("loops", true))
	require.NoError(t, m.Store.Set("usage", false))
	require.NotContains(t, iconSlots(m), slotSpend)

	require.Equal(t, "unseen", slotUnseen.id())
	require.Equal(t, "longrun", slotLongRun.id())
	require.Equal(t, []int{1, 8, 8, colAgents, 6, colRun}, slotWidth[:])
}

func TestRowIconsOn(t *testing.T) {
	m := iconModel(t, "mobile_layout", "off")
	require.True(t, rowIconsOn(m))
	require.NoError(t, m.Store.Set("row_icons", false))
	require.False(t, rowIconsOn(m))
	m = iconModel(t, "mobile_layout", "always")
	require.False(t, rowIconsOn(m))
}
