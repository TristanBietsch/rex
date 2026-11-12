package theme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidName(t *testing.T) {
	for _, ok := range []string{"a", "noir", "my-theme_2", "0day", strings.Repeat("a", MaxNameLen)} {
		require.NoError(t, ValidName(ok), ok)
	}
	for _, bad := range []string{"", "Noir", "-x", "_x", "a b", "a/b", "../x", "é", strings.Repeat("a", MaxNameLen+1)} {
		require.Error(t, ValidName(bad), bad)
	}
}
