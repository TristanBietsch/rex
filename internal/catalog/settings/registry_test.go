package settings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// theme offers the built-in and bundled themes, default first, then the
// opt-in auto last; the default value stays default.
func TestRegistry_ThemeOptionsAreTheBuiltinThemesThenAuto(t *testing.T) {
	e, ok := findEntry("theme")
	require.True(t, ok)
	require.Equal(t, "default", e.Default)
	require.Equal(t, "default", e.Options[0])
	require.Equal(t, theme.AutoName, e.Options[len(e.Options)-1])
	require.ElementsMatch(t, append(theme.Names(), theme.AutoName), e.Options)
	for _, name := range e.Options {
		require.NoError(t, NewStore().Set("theme", name), name)
	}
}

// Every theme option but auto resolves to a theme rex ships, and the bundled
// ones follow the Go built-ins, just before auto.
func TestThemeOptionsAreThemes(t *testing.T) {
	e, ok := findEntry("theme")
	require.True(t, ok)
	for _, name := range e.Options {
		if name != theme.AutoName {
			require.Contains(t, theme.Names(), name)
		}
	}
	require.Equal(t, []string{"ansi", "dracula", "tokyo_night", "pantone_safe", theme.AutoName}, e.Options[len(e.Options)-5:])
}

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

	counts, ok := findEntry("screensaver_counts")
	require.True(t, ok)
	require.Equal(t, TypeEnum, counts.Type)
	require.Equal(t, "horizontal", counts.Default, "today's one-line counts")
	require.Equal(t, []string{"horizontal", "vertical", "off"}, counts.Options)

	clock, ok := findEntry("screensaver_clock")
	require.True(t, ok)
	require.Equal(t, TypeEnum, clock.Type)
	require.Equal(t, "off", clock.Default)
	require.Equal(t, []string{"off", "24h", "12h"}, clock.Options)

	drift, ok := findEntry("screensaver_drift")
	require.True(t, ok)
	require.Equal(t, TypeBool, drift.Type)
	require.Equal(t, true, drift.Default)

	for _, s := range []Setting{on, after, awake, counts, clock, drift} {
		require.Equal(t, SectionBehavior, s.Section, s.ID)
		require.NotEmpty(t, s.Help, s.ID)
		require.False(t, s.ReadOnly, s.ID)
	}
}

func TestRegistry_SkipPermissions(t *testing.T) {
	e, ok := findEntry("skip_permissions")
	require.True(t, ok)
	require.Equal(t, TypeBool, e.Type)
	require.Equal(t, false, e.Default, "opt-in")
	require.Equal(t, SectionBehavior, e.Section)
	require.Contains(t, e.Help, "without asking")
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
