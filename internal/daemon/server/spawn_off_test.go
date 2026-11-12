package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// With extra_agents off the three tools are not in the registry; a spawn says
// the feature is off, not just "not in registry".
func TestSpawnRefusalWhenExtraAgentsOff(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	off := reg.Without(registry.ExtraAgentIDs...)
	err = unknownToolError(off, "qwen", "default")
	require.ErrorContains(t, err, "off: enable in Settings → Features (`rex config set extra_agents true`)")
	require.ErrorContains(t, unknownToolError(off, "nope", "x"), "not in registry")
	require.ErrorContains(t, unknownToolError(reg, "qwen", "nope"), "not in registry", "flag on: ordinary unknown model")
	require.ErrorContains(t, unknownToolError(off.WithModels(nil), "grok", "x"), "off:", "kept through WithModels")
}
