package inspect

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestRunRender_NoDaemonExit4(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	err := RunRender([]string{"-socket", sock})
	var ee core.ExitError
	require.True(t, errors.As(err, &ee), "%v", err)
	require.Equal(t, core.ExitDaemonUnreachable, ee.Code)
	require.Contains(t, ee.Msg, "rex-daemon is not running")
}
