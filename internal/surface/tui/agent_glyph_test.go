package tui

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestExtraAgentGlyphsAvoidMarkers: the icons of qwen, cursor and grok are
// not a state marker, the selection arrow, the loop mark, a spinner frame or
// a waiting frame, so a row or picker never reads as a state.
func TestExtraAgentGlyphsAvoidMarkers(t *testing.T) {
	markers := map[string]bool{selectDefault: true, loopDefault: true, queuedGlyph: true}
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
	for _, id := range registry.ExtraAgentIDs {
		tool, ok := reg.Find(id)
		require.True(t, ok, id)
		require.False(t, markers[tool.Icon], "%s icon %q is a board marker", id, tool.Icon)
	}
}
