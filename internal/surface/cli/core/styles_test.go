package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A bundled theme is not a Go built-in, so resolvePalette reads the (here
// missing) user directory first and then finds the bundled palette.
func TestResolvePaletteBundled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	p := resolvePalette("tokyo_night")
	require.Equal(t, "tokyo_night", p.Name)
}
