package theme

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoPick(t *testing.T) {
	require.Equal(t, DefaultName, AutoPick(true), "a dark terminal gets default")
	require.Equal(t, "paper", AutoPick(false), "a light terminal gets paper")
}

func TestGetAutoIsTheDarkDefault(t *testing.T) {
	// Nothing outside the TUI looks at the terminal, so auto on its own is
	// the dark default, and it is a known name (no "not found" notice).
	p, ok := Get(AutoName)
	require.True(t, ok)
	require.Equal(t, Builtin[DefaultName].Accent, p.Accent)
	require.True(t, p.Dark)

	var cat *Catalog
	p, ok = cat.Get(AutoName)
	require.True(t, ok, "a catalog without user files resolves it too")
	require.Equal(t, Builtin[DefaultName].Accent, p.Accent)
}

func TestAutoIsNotABuiltinPalette(t *testing.T) {
	_, builtin := Builtin[AutoName]
	require.False(t, builtin, "auto is resolved, not a palette of its own")
	require.NotContains(t, Names(), AutoName)
}

func TestParseRefusesTheAutoName(t *testing.T) {
	_, _, err := Parse(AutoName, []byte("extends: noir\n"))
	require.ErrorContains(t, err, "reserved")
}
