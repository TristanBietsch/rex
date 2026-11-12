package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// summary_schedule is a Features switch, on by default (R15).
func TestSummarySchedule_FeatureSwitchOnByDefault(t *testing.T) {
	def, ok := Find("summary_schedule")
	require.True(t, ok)
	require.Equal(t, SectionFeatures, def.Section)
	require.Equal(t, TypeBool, def.Type)
	require.Equal(t, true, def.Default)
	require.Contains(t, def.Help, "5 minutes")
	require.Contains(t, def.Help, "30 minutes")
	require.True(t, FeatureOn(NewStore(), "summary_schedule"))
}
