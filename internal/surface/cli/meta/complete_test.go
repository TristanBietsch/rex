package meta

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestRunComplete_NoSelectorReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{})
	require.Error(t, err)
	ec, ok := err.(core.ExitError)
	require.True(t, ok)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_TooManyArgsReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{"a", "b"})
	require.Error(t, err)
	ec, ok := err.(core.ExitError)
	require.True(t, ok)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_DaemonUnreachable(t *testing.T) {
	err := RunComplete([]string{"--socket", "/tmp/definitely-not-a-real-socket-rex-test", "some-id"})
	require.Error(t, err)
	ec, ok := err.(core.ExitError)
	require.True(t, ok)
	require.Equal(t, core.ExitDaemonUnreachable, ec.ExitCode())
}
