package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry_HasDefaultSpawnTool(t *testing.T) {
	e, ok := findEntry("default_spawn_tool")
	require.True(t, ok, "default_spawn_tool key must be registered")
	require.Equal(t, "claude", e.Default)
}

func TestRegistry_HasDefaultSpawnModel(t *testing.T) {
	e, ok := findEntry("default_spawn_model")
	require.True(t, ok)
	require.Equal(t, "opus", e.Default)
}

func TestRegistry_HasDefaultSpawnEffort(t *testing.T) {
	e, ok := findEntry("default_spawn_effort")
	require.True(t, ok)
	require.Equal(t, "max", e.Default)
}

// findEntry locates a Setting by ID.
func findEntry(id string) (Setting, bool) {
	for _, e := range Registry {
		if e.ID == id {
			return e, true
		}
	}
	return Setting{}, false
}
