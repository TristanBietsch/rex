package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestAgentGlyphsAvoidMarkers: no built-in agent icon is a state marker, the
// selection arrow, the loop mark, a row-strip glyph, a spinner frame or a
// waiting frame, so a row or picker never reads as a state.
func TestAgentGlyphsAvoidMarkers(t *testing.T) {
	markers := map[string]bool{selectDefault: true, loopDefault: true, queuedGlyph: true, "▍": true}
	for _, g := range iconGlyphs {
		markers[g[0]] = true
	}
	for _, st := range []protocol.State{
		protocol.StateNeedsInput, protocol.StateDone, protocol.StateFailed,
		protocol.StateCrashed, protocol.StateQueued,
	} {
		markers[stateGlyph(st)] = true
	}
	for _, frames := range spinnerFrameSets {
		for _, f := range frames {
			markers[f] = true
		}
	}
	for _, set := range waitingSets {
		for _, f := range set.Frames {
			markers[f] = true
		}
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	require.NotEmpty(t, registry.ExtraAgentIDs)
	for _, tool := range reg.Tools {
		require.False(t, markers[tool.Icon], "%s icon %q is a board marker", tool.ID, tool.Icon)
	}
}
