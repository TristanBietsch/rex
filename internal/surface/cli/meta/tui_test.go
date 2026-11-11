package meta

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// Without a home directory (and no XDG_RUNTIME_DIR) the board has no socket
// to dial: rex fails with a clear error before the TUI starts.
func TestRunTUI_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("REX_LOG_DIR", t.TempDir())

	err := RunTUI(false)
	var ee core.ExitError
	require.True(t, errors.As(err, &ee), "%v", err)
	require.Equal(t, core.ExitGeneric, ee.Code)
	require.Contains(t, ee.Msg, "pass -socket")
}
