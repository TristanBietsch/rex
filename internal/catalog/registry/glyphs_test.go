package registry

import (
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestBuiltinGlyphsUnique: every built-in tool has its own icon, one cell
// wide, so the wizard and pickers can tell agents apart at a glance.
func TestBuiltinGlyphsUnique(t *testing.T) {
	reg, err := Load("")
	require.NoError(t, err)
	seen := map[string]string{}
	for _, tool := range reg.Tools {
		require.NotEmpty(t, tool.Icon, tool.ID)
		require.Equal(t, 1, xansi.StringWidth(tool.Icon), "%s icon %q must be one cell wide", tool.ID, tool.Icon)
		if other, dup := seen[tool.Icon]; dup {
			t.Errorf("%s and %s share icon %q", other, tool.ID, tool.Icon)
		}
		seen[tool.Icon] = tool.ID
	}
}
