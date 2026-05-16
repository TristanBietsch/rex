package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunComplete_NoSelectorReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_TooManyArgsReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{"a", "b"})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_DaemonUnreachable(t *testing.T) {
	err := RunComplete([]string{"--socket", "/tmp/definitely-not-a-real-socket-rex-test", "some-id"})
	require.Error(t, err)
	ec, ok := err.(ExitError)
	require.True(t, ok)
	require.Equal(t, ExitDaemonUnreachable, ec.ExitCode())
}
