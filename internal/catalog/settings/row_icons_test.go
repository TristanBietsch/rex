package settings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRowIconsSettings_Defaults(t *testing.T) {
	d, ok := Find("row_icons")
	require.True(t, ok)
	require.Equal(t, SectionFeatures, d.Section)
	require.Equal(t, true, d.Default)
	require.Equal(t, ApplyLive, d.Apply)

	d, ok = Find("row_icon_set")
	require.True(t, ok)
	require.Equal(t, SectionAppearance, d.Section)
	require.Equal(t, TypeString, d.Type)
	require.Equal(t, ",", d.ListSep)
	require.Equal(t, "unseen,reason,loop,subagents,spend,longrun", d.Default)

	d, ok = Find("ascii_glyphs")
	require.True(t, ok)
	require.Equal(t, SectionAppearance, d.Section)
	require.Equal(t, false, d.Default)
}
