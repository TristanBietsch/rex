package setup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rex config get and set say what accent_color paints on the theme, with the
// contrast adjustment; the bare formats keep the stored value.
func TestConfigAccentColorShowsWhatItPaints(t *testing.T) {
	luaConfigHome(t, "", "theme: paper\n")
	_, _, err := runFmt(t, "text", func() error { return configSet([]string{"accent_color", " Peach Fuzz "}) })
	require.NoError(t, err)

	txt, _, err := runFmt(t, "text", func() error { return configGet([]string{"accent_color"}) })
	require.NoError(t, err)
	require.Contains(t, txt, "peach-fuzz")
	require.Regexp(t, `paints #[0-9A-F]{6} on paper, adjusted from #FFBE98 for contrast`, txt)
	plain, _, err := runFmt(t, "txt", func() error { return configGet([]string{"accent_color"}) })
	require.NoError(t, err)
	require.Equal(t, "peach-fuzz\n", plain)
	ag, _, err := runFmt(t, "agent", func() error { return configGet([]string{"accent_color"}) })
	require.NoError(t, err)
	require.Regexp(t, `paints="#[0-9A-F]{6}"`, ag)

	_, _, err = runFmt(t, "text", func() error { return configSet([]string{"accent_color", "rgb(300, 0, 0)"}) })
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range 0-255")

	_, _, err = runFmt(t, "text", func() error { return configSet([]string{"accent_color", "theme"}) })
	require.NoError(t, err)
	txt, _, err = runFmt(t, "text", func() error { return configGet([]string{"accent_color"}) })
	require.NoError(t, err)
	require.NotContains(t, txt, "paints")
}
