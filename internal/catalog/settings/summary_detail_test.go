package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry_SummaryDetail(t *testing.T) {
	e, ok := findEntry("summary_detail")
	require.True(t, ok)
	require.Equal(t, TypeEnum, e.Type)
	require.Equal(t, "medium", e.Default)
	require.Equal(t, []string{"short", "medium", "long"}, e.Options)
	require.Equal(t, SectionSummary, e.Section)
	require.NotEmpty(t, e.Help)

	s := NewStore()
	require.Error(t, s.Set("summary_detail", "huge"))
	require.NoError(t, s.Set("summary_detail", "long"))
	require.Equal(t, "long", s.Get("summary_detail"))
}

// Summary section order (override #6): enabled, model, detail, animation; later
// features append after desc_animation.
func TestRegistry_SummaryOrder(t *testing.T) {
	var ids []string
	for _, s := range Registry {
		if s.Section == SectionSummary {
			ids = append(ids, s.ID)
		}
	}
	require.Equal(t, []string{"summary_enabled", "summary_model", "summary_detail", "desc_animation"}, ids[:4])
}
