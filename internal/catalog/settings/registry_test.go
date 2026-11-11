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

func TestRegistry_Screensaver(t *testing.T) {
	on, ok := findEntry("screensaver_enabled")
	require.True(t, ok)
	require.Equal(t, TypeBool, on.Type)
	require.Equal(t, true, on.Default)

	after, ok := findEntry("screensaver_after_minutes")
	require.True(t, ok)
	require.Equal(t, TypeInt, after.Type)
	require.Equal(t, 5, after.Default)
	require.Equal(t, 1.0, after.Min)
	require.Equal(t, 120.0, after.Max)

	awake, ok := findEntry("screensaver_keep_awake")
	require.True(t, ok)
	require.Equal(t, TypeBool, awake.Type)
	require.Equal(t, true, awake.Default)

	for _, s := range []Setting{on, after, awake} {
		require.Equal(t, SectionBehavior, s.Section, s.ID)
		require.NotEmpty(t, s.Help, s.ID)
		require.False(t, s.ReadOnly, s.ID)
	}
}

func TestStore_ScreensaverMinutesRange(t *testing.T) {
	s := NewStore()
	require.Error(t, s.Set("screensaver_after_minutes", 0))
	require.Error(t, s.Set("screensaver_after_minutes", 121))
	require.NoError(t, s.Set("screensaver_after_minutes", 120))
	require.Equal(t, 120, s.Get("screensaver_after_minutes"))
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
