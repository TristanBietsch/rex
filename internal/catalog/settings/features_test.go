package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// withFeatureRows registers throwaway Features rows for one test.
func withFeatureRows(t *testing.T, rows ...Setting) {
	t.Helper()
	saved := Registry
	Registry = append(append([]Setting(nil), Registry...), rows...)
	t.Cleanup(func() { Registry = saved })
}

// Every Features row is a switch: a bool or an enum, with help text.
func TestRegistry_FeaturesRowsAreSwitches(t *testing.T) {
	for _, s := range Registry {
		if s.Section != SectionFeatures {
			continue
		}
		require.Contains(t, []Type{TypeBool, TypeEnum}, s.Type, "%s must be bool or enum", s.ID)
		require.NotEmpty(t, s.Help, s.ID)
		require.False(t, s.ReadOnly, s.ID)
	}
}

// The Features block sits right after Behavior and before every later section.
func TestRegistry_FeaturesFollowBehavior(t *testing.T) {
	var seenBehavior, seenFeatures, seenLater bool
	for _, s := range Registry {
		switch s.Section {
		case SectionBehavior:
			require.False(t, seenFeatures, "%s: Behavior row after Features", s.ID)
			seenBehavior = true
		case SectionFeatures:
			require.True(t, seenBehavior, "%s: Features before Behavior", s.ID)
			require.False(t, seenLater, "%s: Features row after a later section", s.ID)
			seenFeatures = true
		default:
			if seenBehavior {
				seenLater = true
			}
		}
	}
}

func TestFeatureOn(t *testing.T) {
	withFeatureRows(t,
		Setting{ID: "t_flag_on", Label: "On", Section: SectionFeatures, Type: TypeBool, Default: true, Help: "h"},
		Setting{ID: "t_flag_off", Label: "Off", Section: SectionFeatures, Type: TypeBool, Default: false, Help: "h"},
		Setting{ID: "t_flag_enum", Label: "Enum", Section: SectionFeatures, Type: TypeEnum, Default: "fit",
			Options: []string{"fit", "off"}, Help: "h"},
	)
	s := NewStore()
	require.True(t, FeatureOn(s, "t_flag_on"))
	require.False(t, FeatureOn(s, "t_flag_off"))
	require.True(t, FeatureOn(s, "t_flag_enum"))
	require.True(t, FeatureOn(nil, "t_flag_on"), "nil store reads defaults")

	require.NoError(t, s.Set("t_flag_on", false))
	require.NoError(t, s.Set("t_flag_off", true))
	require.NoError(t, s.Set("t_flag_enum", "off"))
	require.False(t, FeatureOn(s, "t_flag_on"))
	require.True(t, FeatureOn(s, "t_flag_off"))
	require.False(t, FeatureOn(s, "t_flag_enum"))
}

func TestFeatureOn_PanicsOnNonFeature(t *testing.T) {
	require.Panics(t, func() { FeatureOn(nil, "no_such_setting") })
	require.Panics(t, func() { FeatureOn(nil, "summary_enabled") }, "existing toggles keep their own section")
}
