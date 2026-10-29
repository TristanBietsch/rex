package meta

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestRunComplete_NoSelectorReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{})
	var ec core.ExitError
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_TooManyArgsReturnsInvalidArgs(t *testing.T) {
	err := RunComplete([]string{"a", "b"})
	var ec core.ExitError
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_FlagAfterSelector(t *testing.T) {
	err := RunComplete([]string{"some-id", "--socket", "/tmp/definitely-not-a-real-socket-rex-test"})
	var ec core.ExitError
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitDaemonUnreachable, ec.ExitCode())
}

func TestRunCompletion_Arity(t *testing.T) {
	var ec core.ExitError
	require.ErrorAs(t, RunCompletion([]string{"zsh", "bash"}), &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
	require.ErrorAs(t, RunCompletion(nil), &ec)
	require.Equal(t, core.ExitInvalidArgs, ec.ExitCode())
}

func TestRunComplete_DaemonUnreachable(t *testing.T) {
	err := RunComplete([]string{"--socket", "/tmp/definitely-not-a-real-socket-rex-test", "some-id"})
	var ec core.ExitError
	require.ErrorAs(t, err, &ec)
	require.Equal(t, core.ExitDaemonUnreachable, ec.ExitCode())
}
