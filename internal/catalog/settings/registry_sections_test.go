package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The settings page prints a section header whenever Section changes while
// walking Registry in order, so each section must be one contiguous run.
func TestRegistry_SectionsAreContiguous(t *testing.T) {
	closed := map[Section]bool{}
	var cur Section
	for i, s := range Registry {
		if i > 0 && s.Section != cur {
			closed[cur] = true
		}
		require.False(t, closed[s.Section], "%s: section %q appears again after another section", s.ID, s.Section)
		cur = s.Section
	}
}

func TestRegistry_BoardHiddenSections(t *testing.T) {
	e, ok := findEntry("board_hidden_sections")
	require.True(t, ok)
	require.Equal(t, TypeString, e.Type)
	require.Equal(t, "", e.Default)
	require.Equal(t, SectionBehavior, e.Section)
	require.NotEmpty(t, e.Help)

	for i, s := range Registry {
		if s.ID == "screensaver_keep_awake" {
			require.Equal(t, "board_hidden_sections", Registry[i+1].ID, "right after screensaver_keep_awake")
		}
	}
}
